// Package spec owns policy document validation, canonicalization and content identity.
package spec

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

const (
	MaxYAMLBytes           = 64 << 10
	DefaultValidForSeconds = int64(3600)
	// MaxValidForSeconds keeps the schema's only numeric field within the
	// IEEE 754 safe-integer range used by RFC 8785 / ECMAScript consumers.
	MaxValidForSeconds = int64(1<<53 - 1)
)

var (
	keyPattern      = regexp.MustCompile(`^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$`)
	durationPattern = regexp.MustCompile(`^[1-9][0-9]*$`)
)

type Document struct {
	APIVersion string `yaml:"apiVersion" json:"apiVersion"`
	Kind       string `yaml:"kind" json:"kind"`
	Metadata   struct {
		Name string `yaml:"name" json:"name"`
	} `yaml:"metadata" json:"metadata"`
	Spec struct {
		Description     string  `yaml:"description" json:"description"`
		Instructions    string  `yaml:"instructions" json:"instructions"`
		ValidForSeconds *int64  `yaml:"validForSeconds,omitempty" json:"validForSeconds,omitempty"`
		Checks          []Check `yaml:"checks" json:"checks"`
	} `yaml:"spec" json:"spec"`
}
type Check struct {
	ID                   string                `yaml:"id" json:"id"`
	DisplayName          string                `yaml:"displayName" json:"displayName"`
	Description          string                `yaml:"description" json:"description"`
	Instructions         string                `yaml:"instructions" json:"instructions"`
	Criteria             *Criteria             `yaml:"criteria" json:"criteria"`
	EvidenceRequirements []EvidenceRequirement `yaml:"evidenceRequirements" json:"evidenceRequirements"`
}
type Criteria struct {
	Pass    string `yaml:"pass" json:"pass"`
	Block   string `yaml:"block" json:"block"`
	Unknown string `yaml:"unknown" json:"unknown"`
}
type EvidenceRequirement struct {
	ID                string   `yaml:"id" json:"id"`
	Description       string   `yaml:"description" json:"description"`
	AppliesToVerdicts []string `yaml:"appliesToVerdicts" json:"appliesToVerdicts"`
}
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }
func invalid(message string) error       { return &ValidationError{message} }

// Validate accepts the v1alpha1 schema. Validity durations are positive safe
// integers; all other scalar values are strings. No aliases, custom tags,
// unknown fields, implicit scalar coercions or extra YAML documents are accepted.
func Validate(raw string) (string, string, error) {
	if len(raw) > MaxYAMLBytes || !utf8.ValidString(raw) {
		return "", "", invalid("Spec must be UTF-8 and at most 64 KiB")
	}
	var root yaml.Node
	decoder := yaml.NewDecoder(strings.NewReader(raw))
	if decoder.Decode(&root) != nil {
		return "", "", invalid("Spec must contain one valid YAML document")
	}
	if decoder.Decode(new(yaml.Node)) != io.EOF {
		return "", "", invalid("Only one YAML document is allowed")
	}
	count := 0
	if err := validateNode(&root, 0, &count, nil); err != nil {
		return "", "", err
	}
	var doc Document
	decoder = yaml.NewDecoder(strings.NewReader(raw))
	decoder.KnownFields(true)
	if decoder.Decode(&doc) != nil {
		return "", "", invalid("Spec contains unknown fields or incorrect field types")
	}
	if doc.APIVersion != "guardrail.open/v1alpha1" || doc.Kind != "GuardrailSpec" || !keyPattern.MatchString(doc.Metadata.Name) {
		return "", "", invalid("Expected apiVersion guardrail.open/v1alpha1, kind GuardrailSpec and a valid metadata.name")
	}
	if err := validateText(doc.Spec.Description, "spec.description", 1024); err != nil {
		return "", "", err
	}
	if err := validateText(doc.Spec.Instructions, "spec.instructions", 8000); err != nil {
		return "", "", err
	}
	if doc.Spec.ValidForSeconds == nil {
		defaultValidity := DefaultValidForSeconds
		doc.Spec.ValidForSeconds = &defaultValidity
	}
	if err := validateValidity(*doc.Spec.ValidForSeconds); err != nil {
		return "", "", err
	}
	if len(doc.Spec.Checks) == 0 || len(doc.Spec.Checks) > 20 {
		return "", "", invalid("spec.checks must contain 1–20 checks")
	}
	ids := map[string]bool{}
	for i, check := range doc.Spec.Checks {
		if !keyPattern.MatchString(check.ID) || ids[check.ID] {
			return "", "", invalid(fmt.Sprintf("Check %d needs a unique valid id", i+1))
		}
		ids[check.ID] = true
		if err := validateText(check.DisplayName, "check.displayName", 128); err != nil {
			return "", "", err
		}
		if err := validateText(check.Description, "check.description", 1024); err != nil {
			return "", "", err
		}
		if err := validateText(check.Instructions, "check.instructions", 8000); err != nil {
			return "", "", err
		}
		if check.Criteria == nil {
			return "", "", invalid(fmt.Sprintf("Check %d needs criteria", i+1))
		}
		for _, criterion := range []struct{ field, value string }{
			{"pass", check.Criteria.Pass},
			{"block", check.Criteria.Block},
			{"unknown", check.Criteria.Unknown},
		} {
			if err := validateText(criterion.value, "check.criteria."+criterion.field, 2048); err != nil {
				return "", "", err
			}
		}
		if len(check.EvidenceRequirements) == 0 || len(check.EvidenceRequirements) > 20 {
			return "", "", invalid(fmt.Sprintf("Check %d needs 1–20 evidence requirements", i+1))
		}
		evidenceIDs := map[string]bool{}
		for _, req := range check.EvidenceRequirements {
			if !keyPattern.MatchString(req.ID) || evidenceIDs[req.ID] {
				return "", "", invalid(fmt.Sprintf("Check %d needs unique valid evidence ids", i+1))
			}
			evidenceIDs[req.ID] = true
			if err := validateText(req.Description, "evidence.description", 1024); err != nil {
				return "", "", err
			}
			if len(req.AppliesToVerdicts) == 0 {
				return "", "", invalid("GuardrailSpec evidence requires appliesToVerdicts")
			}
			verdicts := map[string]bool{}
			for _, v := range req.AppliesToVerdicts {
				if (v != "PASS" && v != "BLOCKED" && v != "UNKNOWN") || verdicts[v] {
					return "", "", invalid("Evidence appliesToVerdicts must contain unique PASS, BLOCKED or UNKNOWN values")
				}
				verdicts[v] = true
			}
		}
	}
	data, _ := json.Marshal(doc)
	var tree any
	jsonDecoder := json.NewDecoder(bytes.NewReader(data))
	jsonDecoder.UseNumber()
	_ = jsonDecoder.Decode(&tree)
	var out strings.Builder
	canonicalValue(&out, tree)
	canonical := out.String()
	sum := sha256.Sum256([]byte(canonical))
	return canonical, hex.EncodeToString(sum[:]), nil
}
func validateText(value, path string, maxLength int) error {
	if strings.TrimSpace(value) == "" || utf8.RuneCountInString(value) > maxLength {
		return invalid(fmt.Sprintf("%s must be non-blank and at most %d characters", path, maxLength))
	}
	return nil
}

func validityIntegerPath(path []string) bool {
	return len(path) == 2 && path[0] == "spec" && path[1] == "validForSeconds"
}

func validateValidity(seconds int64) error {
	if seconds < 1 || seconds > MaxValidForSeconds {
		return invalid(fmt.Sprintf("spec.validForSeconds must be an integer from 1 to %d", MaxValidForSeconds))
	}
	return nil
}

func validateNode(n *yaml.Node, depth int, count *int, path []string) error {
	*count += 1
	if depth > 16 || *count > 4096 {
		return invalid("Spec nesting or node limit exceeded")
	}
	if n.Kind == yaml.AliasNode || n.Anchor != "" {
		return invalid("YAML anchors and aliases are not supported")
	}
	switch n.Kind {
	case yaml.DocumentNode:
	case yaml.MappingNode:
		if n.Tag != "!!map" {
			return invalid("Custom YAML tags are not supported")
		}
		keys := map[string]bool{}
		for i := 0; i < len(n.Content); i += 2 {
			key := n.Content[i]
			if key.Tag != "!!str" || key.Kind != yaml.ScalarNode || keys[key.Value] {
				return invalid("YAML mapping keys must be unique strings")
			}
			keys[key.Value] = true
			if err := validateNode(key, depth+1, count, path); err != nil {
				return err
			}
			if err := validateNode(n.Content[i+1], depth+1, count, append(path, key.Value)); err != nil {
				return err
			}
		}
		return nil
	case yaml.SequenceNode:
		if n.Tag != "!!seq" {
			return invalid("Custom YAML tags are not supported")
		}
		for _, child := range n.Content {
			if err := validateNode(child, depth+1, count, path); err != nil {
				return err
			}
		}
		return nil
	case yaml.ScalarNode:
		if validityIntegerPath(path) {
			if n.Tag != "!!int" || !durationPattern.MatchString(n.Value) {
				return invalid("Spec validForSeconds must be a positive base-10 integer without leading zeros")
			}
			seconds, err := strconv.ParseInt(n.Value, 10, 64)
			if err != nil {
				return invalid(fmt.Sprintf("spec.validForSeconds must be an integer from 1 to %d", MaxValidForSeconds))
			}
			return validateValidity(seconds)
		}
		if n.Tag != "!!str" {
			return invalid("Spec scalar values must be strings")
		}
	default:
		return invalid("Unsupported YAML structure")
	}
	for _, child := range n.Content {
		if err := validateNode(child, depth+1, count, path); err != nil {
			return err
		}
	}
	return nil
}

// This schema has fixed ASCII object keys, arrays, UTF-8 strings and a positive
// safe-integer validity duration. Validated numbers are exact in IEEE 754 double
// precision and below ECMAScript's exponential-notation threshold, so their decimal
// integer spelling is also their RFC 8785 representation. Keep this serializer
// restricted to the validated schema; it is not a general-purpose JCS encoder.
func canonicalValue(out *strings.Builder, value any) {
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				out.WriteByte(',')
			}
			canonicalValue(out, k)
			out.WriteByte(':')
			canonicalValue(out, v[k])
		}
		out.WriteByte('}')
	case []any:
		out.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				out.WriteByte(',')
			}
			canonicalValue(out, item)
		}
		out.WriteByte(']')
	case string:
		out.WriteByte('"')
		for _, r := range v {
			switch r {
			case '"', '\\':
				out.WriteByte('\\')
				out.WriteRune(r)
			case '\b':
				out.WriteString("\\b")
			case '\t':
				out.WriteString("\\t")
			case '\n':
				out.WriteString("\\n")
			case '\f':
				out.WriteString("\\f")
			case '\r':
				out.WriteString("\\r")
			default:
				if r < 0x20 {
					fmt.Fprintf(out, "\\u%04x", r)
				} else {
					out.WriteRune(r)
				}
			}
		}
		out.WriteByte('"')
	case json.Number:
		out.WriteString(v.String())
	}
}

func ResolveValidForSeconds(canonical string) (int64, error) {
	var document struct {
		Spec struct {
			ValidForSeconds *int64 `json:"validForSeconds"`
		} `json:"spec"`
	}
	if err := json.Unmarshal([]byte(canonical), &document); err != nil {
		return 0, err
	}
	if document.Spec.ValidForSeconds == nil {
		return DefaultValidForSeconds, nil
	}
	if err := validateValidity(*document.Spec.ValidForSeconds); err != nil {
		return 0, err
	}
	return *document.Spec.ValidForSeconds, nil
}
