package run

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func FuzzCanonicalizeChangeSet(f *testing.F) {
	for _, raw := range []string{
		`{}`, `{"nested":{"values":[1,1.0,-0,1e2,null,true]},"name":"发布"}`,
		`{"b":"<>&\u2028\u2029","a":"\u0000\"\\"}`,
		`{"x":1,"\u0078":2}`, `{"nested":[{"x":1,"x":2}]}`,
		`{"n":1e9999}`, `{"text":"\ud800"}`, `{} {}`, `[]`, `null`, "", "\xff",
		`{"nested":` + strings.Repeat("[", maxJSONDepth) + `0` + strings.Repeat("]", maxJSONDepth) + `}`,
		`{"nodes":[` + strings.Repeat("0,", maxJSONNodes) + `0]}`,
		strings.Repeat(" ", MaxChangeSetBytes+1),
	} {
		f.Add([]byte(raw))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		canonical, digest, err := Canonicalize(raw)
		if err != nil {
			assertInvalidChangeSet(t, canonical, digest, err)
			return
		}
		if len(raw) > MaxChangeSetBytes || !utf8.Valid(raw) {
			t.Fatal("accepted input outside the byte/UTF-8 limits")
		}
		if !utf8.Valid(canonical) || !json.Valid(canonical) {
			t.Fatal("canonical ChangeSet is not UTF-8 JSON")
		}
		sum := sha256.Sum256(canonical)
		if digest != hex.EncodeToString(sum[:]) {
			t.Fatal("digest does not identify the canonical bytes")
		}
		// Use the standard library's map encoder as an independent oracle.
		// UseNumber preserves the contract's distinction between 1 and 1.0.
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var object map[string]any
		if err := decoder.Decode(&object); err != nil || object == nil {
			t.Fatalf("accepted input is not a JSON object: %v", err)
		}
		expected, err := json.Marshal(object)
		if err != nil || !bytes.Equal(canonical, expected) {
			t.Fatalf("canonical form differs from the standard encoder: %v", err)
		}
		assertSameChangeSet(t, raw, canonical, digest)
		if len(canonical) <= MaxChangeSetBytes {
			assertSameChangeSet(t, canonical, canonical, digest)
		}
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, raw, "", "  "); err != nil {
			t.Fatal(err)
		}
		if pretty.Len() <= MaxChangeSetBytes {
			assertSameChangeSet(t, pretty.Bytes(), canonical, digest)
		}
		if len(raw)+3 <= MaxChangeSetBytes {
			trailing := append(append([]byte(nil), raw...), ' ', '{', '}')
			out, outDigest, err := Canonicalize(trailing)
			assertInvalidChangeSet(t, out, outDigest, err)
		}
	})
}

func FuzzRejectDuplicateChangeSetKeys(f *testing.F) {
	for _, key := range []string{"", "发布", "\u0000\"\\", "\ufffd", "<>&\u2028\u2029"} {
		f.Add(key, key)
	}
	f.Fuzz(func(t *testing.T, key, value string) {
		bounded := func(s string) string {
			runes := []rune(strings.ToValidUTF8(s, "\ufffd"))
			if len(runes) > 256 {
				runes = runes[:256]
			}
			return string(runes)
		}
		encodedKey, err := json.Marshal("fuzz" + bounded(key))
		if err != nil {
			t.Fatal(err)
		}
		encodedValue, err := json.Marshal(bounded(value))
		if err != nil {
			t.Fatal(err)
		}
		member := string(encodedKey) + ":" + string(encodedValue)
		if _, _, err := Canonicalize([]byte("{" + member + "}")); err != nil {
			t.Fatalf("unique-key baseline rejected: %v", err)
		}
		// Both spellings decode to the same key, including arbitrary suffixes.
		escapedKey := `"\u0066` + string(encodedKey[2:])
		for _, duplicate := range []string{member, escapedKey + ":null"} {
			object := "{" + member + "," + duplicate + "}"
			for _, raw := range []string{object, `{"nested":` + object + `}`, `{"nested":[` + object + `]}`} {
				canonical, digest, err := Canonicalize([]byte(raw))
				assertInvalidChangeSet(t, canonical, digest, err)
			}
		}
	})
}

func assertSameChangeSet(t *testing.T, raw, canonical []byte, digest string) {
	t.Helper()
	again, againDigest, err := Canonicalize(raw)
	if err != nil || !bytes.Equal(again, canonical) || againDigest != digest {
		t.Fatalf("content identity changed: %v", err)
	}
}

func assertInvalidChangeSet(t *testing.T, canonical []byte, digest string, err error) {
	t.Helper()
	if !errors.Is(err, ErrInvalidChangeSet) || len(canonical) != 0 || digest != "" {
		t.Fatalf("expected rejection without an identity, got %v", err)
	}
}
