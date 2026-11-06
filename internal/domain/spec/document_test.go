package spec

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

const validSpec = `apiVersion: guardrail.open/v1alpha1
kind: GuardrailSpec
metadata:
  name: recovery
spec:
  description: Check recovery readiness
  instructions: Inspect current state and provide evidence
  validForSeconds: 900
  checks:
    - id: readiness
      displayName: Recovery readiness
      description: Check recovery readiness
      instructions: Inspect current health and provide evidence
      criteria:
        pass: Health is good
        block: Health is bad
        unknown: Health is not verified
      evidenceRequirements:
        - id: health
          description: Record current health
          appliesToVerdicts: [PASS, BLOCKED, UNKNOWN]
`

func TestRecoveryExample(t *testing.T) {
	raw, err := os.ReadFile("../../../examples/policies/recovery.yaml")
	if err != nil {
		t.Fatal(err)
	}
	canonical, _, err := Validate(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	validity, err := ResolveValidForSeconds(canonical)
	if err != nil || validity != 900 {
		t.Fatalf("validity=%d err=%v", validity, err)
	}
}

func TestSpecCanonicalization(t *testing.T) {
	canonical, digest, err := Validate(validSpec)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"kind":"GuardrailSpec"`, `"displayName":"Recovery readiness"`, `"criteria":{"block":"Health is bad","pass":"Health is good","unknown":"Health is not verified"}`, `"appliesToVerdicts":["PASS","BLOCKED","UNKNOWN"]`} {
		if !strings.Contains(canonical, field) {
			t.Fatalf("missing %s in %s", field, canonical)
		}
	}
	sum := sha256.Sum256([]byte(canonical))
	if digest != hex.EncodeToString(sum[:]) {
		t.Fatal("wrong digest")
	}
	_, again, err := Validate("# comment\n" + strings.Replace(validSpec, "kind: GuardrailSpec", "kind: \"GuardrailSpec\"", 1))
	if err != nil || again != digest {
		t.Fatal("formatting changed digest", err)
	}
	_, changed, err := Validate(strings.Replace(validSpec, "current health", "recent health", 1))
	if err != nil || changed == digest {
		t.Fatal("content did not change digest", err)
	}
	validity, err := ResolveValidForSeconds(canonical)
	if err != nil || validity != 900 {
		t.Fatalf("validity=%d err=%v", validity, err)
	}
	var out strings.Builder
	canonicalValue(&out, "<>&\u2028\u2029\n\t\b\f\r\x00\"\\")
	if out.String() != "\"<>&\u2028\u2029\\n\\t\\b\\f\\r\\u0000\\\"\\\\\"" {
		t.Fatalf("string encoding: %q", out.String())
	}
}

func TestSpecValidityDefaultAndExistingDurations(t *testing.T) {
	omitted := strings.Replace(validSpec, "  validForSeconds: 900\n", "", 1)
	defaultCanonical, defaultDigest, err := Validate(omitted)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(defaultCanonical, `"validForSeconds":3600`) {
		t.Fatalf("default validity missing from canonical Spec: %s", defaultCanonical)
	}
	validity, err := ResolveValidForSeconds(defaultCanonical)
	if err != nil || validity != DefaultValidForSeconds {
		t.Fatalf("default validity=%d err=%v", validity, err)
	}
	explicitDefault := strings.Replace(validSpec, "validForSeconds: 900", "validForSeconds: 3600", 1)
	explicitCanonical, explicitDigest, err := Validate(explicitDefault)
	if err != nil || explicitCanonical != defaultCanonical || explicitDigest != defaultDigest {
		t.Fatalf("explicit default differs from omitted validity: err=%v", err)
	}
	longer := strings.Replace(validSpec, "validForSeconds: 900", "validForSeconds: 7200", 1)
	longerCanonical, _, err := Validate(longer)
	if err != nil {
		t.Fatal(err)
	}
	validity, err = ResolveValidForSeconds(longerCanonical)
	if err != nil || validity != 7200 {
		t.Fatalf("longer validity=%d err=%v", validity, err)
	}
}

func TestSpecValidation(t *testing.T) {
	cases := map[string]string{
		"empty":                        "",
		"duplicate mapping":            strings.Replace(validSpec, "kind: GuardrailSpec", "kind: GuardrailSpec\nkind: GuardrailSpec", 1),
		"extra document":               validSpec + "\n---\na: b\n",
		"unknown field":                validSpec + "extra: value\n",
		"alias":                        strings.Replace(validSpec, "name: recovery", "name: &name recovery", 1),
		"custom tag":                   strings.Replace(validSpec, "name: recovery", "name: !custom recovery", 1),
		"implicit number":              strings.Replace(validSpec, "id: readiness", "id: 123", 1),
		"invalid name":                 strings.Replace(validSpec, "name: recovery", "name: Recovery", 1),
		"empty checks":                 strings.Replace(validSpec, "  checks:\n"+strings.Split(validSpec, "  checks:\n")[1], "  checks: []\n", 1),
		"missing description":          strings.Replace(validSpec, "  description: Check recovery readiness\n", "", 1),
		"missing instructions":         strings.Replace(validSpec, "  instructions: Inspect current state and provide evidence\n", "", 1),
		"zero validity":                strings.Replace(validSpec, "validForSeconds: 900", "validForSeconds: 0", 1),
		"octal validity":               strings.Replace(validSpec, "validForSeconds: 900", "validForSeconds: 0600", 1),
		"quoted validity":              strings.Replace(validSpec, "validForSeconds: 900", "validForSeconds: \"900\"", 1),
		"invalid verdict":              strings.Replace(validSpec, "UNKNOWN]", "ALLOW]", 1),
		"duplicate verdict":            strings.Replace(validSpec, "UNKNOWN]", "PASS]", 1),
		"missing display name":         strings.Replace(validSpec, "      displayName: Recovery readiness\n", "", 1),
		"missing criteria":             strings.Replace(validSpec, "      criteria:\n        pass: Health is good\n        block: Health is bad\n        unknown: Health is not verified\n", "", 1),
		"missing unknown criteria":     strings.Replace(validSpec, "        unknown: Health is not verified\n", "", 1),
		"missing evidence description": strings.Replace(validSpec, "          description: Record current health\n", "", 1),
		"missing evidence verdicts":    strings.Replace(validSpec, "          appliesToVerdicts: [PASS, BLOCKED, UNKNOWN]\n", "", 1),
		"duplicate evidence":           validSpec + "        - id: health\n          description: Duplicate\n          appliesToVerdicts: [PASS]\n",
		"duplicate check":              validSpec + strings.Split(validSpec, "  checks:\n")[1],
		"oversize":                     strings.Repeat("a", MaxYAMLBytes+1),
		"invalid utf8":                 string([]byte{0xff}),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := Validate(raw)
			var validation *ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("expected validation error, got %v", err)
			}
		})
	}
}

func TestCriteriaValidationOrder(t *testing.T) {
	raw := strings.ReplaceAll(validSpec, "Health is good", `" "`)
	raw = strings.ReplaceAll(raw, "Health is bad", `" "`)
	raw = strings.ReplaceAll(raw, "Health is not verified", `" "`)
	for range 20 {
		_, _, err := Validate(raw)
		if err == nil || err.Error() != "check.criteria.pass must be non-blank and at most 2048 characters" {
			t.Fatalf("unexpected first criteria error: %v", err)
		}
	}
}

// These pinned canonical bytes/digests are also checked independently by Node.js.
// They ensure the implementation cannot silently change identity while fixing a
// number boundary, and include the largest supported exact integer.
func TestSpecCanonicalIdentityVectors(t *testing.T) {
	data, err := os.ReadFile("testdata/canonical-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors []struct {
		Name            string  `json:"name"`
		ValidForSeconds *string `json:"validForSeconds"`
		Canonical       string  `json:"canonical"`
		SHA256          string  `json:"sha256"`
	}
	if err = json.Unmarshal(data, &vectors); err != nil {
		t.Fatal(err)
	}
	if len(vectors) == 0 {
		t.Fatal("no canonical identity vectors")
	}
	for _, vector := range vectors {
		t.Run(vector.Name, func(t *testing.T) {
			raw := strings.Replace(validSpec, "  validForSeconds: 900\n", "", 1)
			if vector.ValidForSeconds != nil {
				raw = strings.Replace(validSpec, "validForSeconds: 900", "validForSeconds: "+*vector.ValidForSeconds, 1)
			}
			canonical, digest, err := Validate(raw)
			if err != nil || canonical != vector.Canonical || digest != vector.SHA256 {
				t.Fatalf("canonical identity changed: canonical=%s digest=%s err=%v", canonical, digest, err)
			}
			got, err := ResolveValidForSeconds(canonical)
			if err != nil {
				t.Fatal(err)
			}
			expected := fmt.Sprintf("%d", DefaultValidForSeconds)
			if vector.ValidForSeconds != nil {
				expected = *vector.ValidForSeconds
			}
			if fmt.Sprintf("%d", got) != expected {
				t.Fatalf("validity changed: %d want %s", got, expected)
			}
		})
	}
}

func TestSpecRejectsUnsafeValidityWithoutCanonicalizing(t *testing.T) {
	for _, number := range []string{"9007199254740992", "9007199254740993", "9007199254740994", "9223372036854775807", "9223372036854775808", "999999999999999999999999999999999"} {
		t.Run(number, func(t *testing.T) {
			canonical, digest, err := Validate(strings.Replace(validSpec, "validForSeconds: 900", "validForSeconds: "+number, 1))
			var invalidSpec *ValidationError
			if !errors.As(err, &invalidSpec) || canonical != "" || digest != "" {
				t.Fatalf("unsafe validity produced identity: %s %s %v", canonical, digest, err)
			}
			if _, err = ResolveValidForSeconds(`{"spec":{"validForSeconds":` + number + `}}`); err == nil {
				t.Fatal("stored canonical Spec accepted unsafe validity")
			}
		})
	}
	for _, number := range []string{"0", "-1", "1.5", "1e3"} {
		if _, err := ResolveValidForSeconds(`{"spec":{"validForSeconds":` + number + `}}`); err == nil {
			t.Fatalf("stored canonical Spec accepted nonpositive or noninteger validity %s", number)
		}
	}
}
