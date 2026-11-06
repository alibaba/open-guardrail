package run

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/alibaba/open-guardrail/internal/domain/spec"
)

const MaxSubmissionBytes = 1 << 20

var (
	ErrInvalidSubmission       = errors.New("snapshot does not satisfy the Policy Revision")
	ErrSensitiveEvidence       = errors.New("snapshot contains sensitive evidence")
	bearerSecretPattern        = regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9._~+/=-]{16,}`)
	guardrailCredentialPattern = regexp.MustCompile(`\b(?:ogk_|ogrt_|ogs_|ogb_)[a-zA-Z0-9_-]{12,}`)
	privateKeyPattern          = regexp.MustCompile(`(?i)-----BEGIN(?: [A-Z0-9]+)* PRIVATE KEY-----`)
)

func validVerdict(value string) bool {
	return value == "PASS" || value == "BLOCKED" || value == "UNKNOWN"
}

func appliesTo(requirement spec.EvidenceRequirement, verdict string) bool {
	for _, candidate := range requirement.AppliesToVerdicts {
		if candidate == verdict {
			return true
		}
	}
	return false
}

func containsSensitive(value any) bool {
	return sensitiveValue(value, 0)
}

func sensitiveValue(value any, depth int) bool {
	if depth > maxJSONDepth {
		return true
	}
	switch v := value.(type) {
	case object:
		for key, item := range v {
			if sensitiveField(key, item) {
				return true
			}
			if sensitiveValue(item, depth+1) {
				return true
			}
		}
	case []any:
		for _, item := range v {
			if sensitiveValue(item, depth+1) {
				return true
			}
		}
	case string:
		if privateKeyPattern.MatchString(v) || bearerSecretPattern.MatchString(v) || guardrailCredentialPattern.MatchString(v) {
			return true
		}
		// Evidence summaries may carry JSON observations, including nested JSON
		// strings. Inspect their fields without changing the persisted text.
		if json.Valid([]byte(v)) {
			decoder := json.NewDecoder(strings.NewReader(v))
			decoder.UseNumber()
			nodes := 0
			decoded, err := decodeValue(decoder, 0, &nodes)
			// Ambiguous duplicate keys and excessive structure must not hide secrets.
			return err != nil || sensitiveValue(decoded, depth+1)
		}
	}
	return false
}

func sensitiveField(key string, value any) bool {
	switch candidate := value.(type) {
	case string:
		return strings.TrimSpace(candidate) != "" && sensitiveStringKey(key)
	case object, []any:
		// Keep rejecting structured credential containers such as
		// {"credentials":{"value":"..."}} even when their nested keys are generic.
		return sensitiveKey(key)
	default:
		return false
	}
}

func sensitiveStringKey(key string) bool {
	normalized := normalizeKey(key)
	if sensitiveKeyNormalized(normalized) {
		return true
	}
	for _, suffix := range []string{
		"password", "passwd", "secret", "apikey", "authtoken", "accesstoken", "refreshtoken",
		"privatekey", "accesskey", "accesskeyid", "accesskeysecret", "credential", "credentials",
	} {
		if strings.HasSuffix(normalized, suffix) {
			return true
		}
	}
	return false
}

func sensitiveKey(key string) bool {
	return sensitiveKeyNormalized(normalizeKey(key))
}

func normalizeKey(key string) string {
	return strings.NewReplacer("_", "", "-", "", ".", "").Replace(strings.ToLower(key))
}

func sensitiveKeyNormalized(normalized string) bool {
	switch normalized {
	case "authorization", "password", "passwd", "token", "secret", "secretkey", "apikey",
		"accesstoken", "refreshtoken", "clientsecret", "privatekey", "cookie", "setcookie",
		"accesskey", "accesskeyid", "accesskeysecret", "secretaccesskey", "awssecretaccesskey",
		"credential", "credentials", "signingkey", "serviceaccountkey":
		return true
	}
	return false
}
