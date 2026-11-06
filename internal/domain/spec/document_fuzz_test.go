package spec

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

func FuzzValidateSpec(f *testing.F) {
	for _, raw := range []string{
		validSpec, "\ufeff" + validSpec, "", "spec: [", "\xff",
		strings.Replace(validSpec, "name: recovery", "name: &name recovery", 1),
		strings.Replace(validSpec, "kind: GuardrailSpec", "kind: GuardrailSpec\nkind: GuardrailSpec", 1),
		strings.Replace(validSpec, "validForSeconds: 900", "validForSeconds: 9007199254740992", 1),
		strings.Replace(validSpec, "  validForSeconds: 900\n", "", 1),
		validSpec + "---\n{}\n",
		strings.Repeat("x", MaxYAMLBytes+1),
	} {
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		canonical, digest, err := Validate(raw)
		if err != nil {
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) || canonical != "" || digest != "" {
				t.Fatalf("invalid input returned an identity or unexpected error: %v", err)
			}
			return
		}
		if len(raw) > MaxYAMLBytes || !utf8.ValidString(raw) {
			t.Fatal("accepted input outside the byte/UTF-8 limits")
		}
		assertSpecIdentity(t, canonical, digest)
		again, againDigest, err := Validate(raw)
		if err != nil || again != canonical || againDigest != digest {
			t.Fatalf("validation is not deterministic: %v", err)
		}
		const comment = "# fuzz formatting\n"
		if len(raw)+len(comment) <= MaxYAMLBytes {
			formatted := comment + raw
			if strings.HasPrefix(raw, "\ufeff") {
				formatted = "\ufeff" + comment + strings.TrimPrefix(raw, "\ufeff")
			}
			again, againDigest, err = Validate(formatted)
			if err != nil || again != canonical || againDigest != digest {
				t.Fatalf("a YAML comment changed content identity: %v", err)
			}
		}
		// Re-encode through JSON so YAML-sensitive line separators are escaped.
		// The canonical form can be larger than the input byte budget.
		var doc Document
		if err := json.Unmarshal([]byte(canonical), &doc); err != nil {
			t.Fatal(err)
		}
		encoded := encodeSpecJSON(t, doc)
		if len(encoded) <= MaxYAMLBytes {
			again, againDigest, err = Validate(encoded)
			if err != nil || again != canonical || againDigest != digest {
				t.Fatalf("JSON round trip changed content identity: %v", err)
			}
		}
	})
}

func FuzzSpecTextIdentity(f *testing.F) {
	var base Document
	if err := yaml.Unmarshal([]byte(validSpec), &base); err != nil {
		f.Fatal(err)
	}
	for _, text := range []string{"", "发布就绪", "<>&\u0085\u2028\u2029", "\x00\n\r\t\"\\", "\x7f\u0080\u009f\ufffe\uffff", "\xff", strings.Repeat("界", 1020)} {
		for _, validity := range []uint64{0, uint64(MaxValidForSeconds - 1), ^uint64(0)} {
			f.Add(text, validity)
		}
	}
	f.Fuzz(func(t *testing.T, text string, duration uint64) {
		// Mutate only value fields; the shared slices in base stay read-only.
		doc := base
		runes := []rune(strings.ToValidUTF8(text, "\ufffd"))
		if len(runes) > 1019 {
			runes = runes[:1019]
		}
		doc.Spec.Description = "fuzz:" + string(runes)
		validity := int64(duration%uint64(MaxValidForSeconds)) + 1
		doc.Spec.ValidForSeconds = &validity
		yamlBytes, err := yaml.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		jsonText := encodeSpecJSON(t, doc)
		canonical, digest, err := Validate(string(yamlBytes))
		if err != nil {
			t.Fatalf("generated YAML document rejected: %v", err)
		}
		assertSpecIdentity(t, canonical, digest)
		other, otherDigest, err := Validate(jsonText)
		if err != nil || other != canonical || otherDigest != digest {
			t.Fatalf("YAML/JSON representations disagree: %v", err)
		}
		var decoded Document
		if err := json.Unmarshal([]byte(canonical), &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.Spec.Description != doc.Spec.Description {
			t.Fatal("canonicalization changed text content")
		}
		resolved, err := ResolveValidForSeconds(canonical)
		if err != nil || resolved != validity {
			t.Fatalf("validity changed: got %d, want %d, err=%v", resolved, validity, err)
		}
		doc.Spec.Description = "Fuzz:" + string(runes)
		_, changedDigest, err := Validate(encodeSpecJSON(t, doc))
		if err != nil || changedDigest == digest {
			t.Fatalf("changed text did not change identity: %v", err)
		}
	})
}

func encodeSpecJSON(t *testing.T, doc Document) string {
	t.Helper()
	encoded, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	// JSON permits literal characters that yaml/v3 rejects or treats as line
	// breaks. Escape them so both documents represent the same original text.
	var out strings.Builder
	for _, r := range string(encoded) {
		if r >= 0x7f && r <= 0x9f || r == 0xfffe || r == 0xffff {
			fmt.Fprintf(&out, `\u%04x`, r)
		} else {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func assertSpecIdentity(t *testing.T, canonical, digest string) {
	t.Helper()
	if !utf8.ValidString(canonical) || !json.Valid([]byte(canonical)) {
		t.Fatal("canonical Spec is not UTF-8 JSON")
	}
	sum := sha256.Sum256([]byte(canonical))
	if digest != hex.EncodeToString(sum[:]) {
		t.Fatal("digest does not identify the canonical bytes")
	}
}
