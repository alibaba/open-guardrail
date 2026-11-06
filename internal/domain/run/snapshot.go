package run

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/alibaba/open-guardrail/internal/domain/spec"
)

const (
	MaxStructuredEvidenceBytes = 16 << 10
	MaxEvidenceCount           = 100
	MaxSummaryRunes            = 2048
	MaxIdentifierRunes         = 128
)

var ErrVerdictMismatch = errors.New("declared verdict differs from evaluated verdict")

type Observation struct {
	RequirementID string  `json:"requirementId"`
	ObservedAt    string  `json:"observedAt"`
	QuerySummary  string  `json:"querySummary"`
	ResultSummary string  `json:"resultSummary"`
	Reference     *string `json:"reference,omitempty"`
}
type CheckSnapshot struct {
	CheckID  string        `json:"checkId"`
	Verdict  string        `json:"verdict"`
	Summary  string        `json:"summary"`
	Evidence []Observation `json:"evidence"`
}
type SubmissionSnapshot struct {
	BaseSubmissionNo int64           `json:"baseSubmissionNo"`
	FinishRun        bool            `json:"finishRun"`
	Verdict          string          `json:"verdict"`
	Summary          string          `json:"summary"`
	Checks           []CheckSnapshot `json:"checks"`
}
type EvaluatedSnapshot struct {
	Snapshot        SubmissionSnapshot
	CanonicalJSON   string
	RequestDigest   string
	Verdict         string
	Decision        string
	EvidenceDigests [][]string
}

// EvaluateSnapshot validates a complete observation against an immutable PolicyRevision.
// The declared top-level verdict is never trusted as the source of the decision.
func EvaluateSnapshot(specCanonicalJSON string, in SubmissionSnapshot) (EvaluatedSnapshot, error) {
	if in.BaseSubmissionNo < 0 || !validVerdict(in.Verdict) || (in.Summary != "" && !validText(in.Summary, MaxSummaryRunes)) {
		return EvaluatedSnapshot{}, ErrInvalidSubmission
	}
	if containsSensitive(in.Summary) {
		return EvaluatedSnapshot{}, ErrSensitiveEvidence
	}
	var doc spec.Document
	if json.Unmarshal([]byte(specCanonicalJSON), &doc) != nil || len(doc.Spec.Checks) == 0 || len(doc.Spec.Checks) != len(in.Checks) {
		return EvaluatedSnapshot{}, ErrInvalidSubmission
	}
	provided := make(map[string]CheckSnapshot, len(in.Checks))
	for _, check := range in.Checks {
		if _, exists := provided[check.CheckID]; exists || !validID(check.CheckID) || !validVerdict(check.Verdict) || !validText(check.Summary, MaxSummaryRunes) {
			return EvaluatedSnapshot{}, ErrInvalidSubmission
		}
		if containsSensitive(check.Summary) {
			return EvaluatedSnapshot{}, ErrSensitiveEvidence
		}
		provided[check.CheckID] = check
	}
	result := "PASS"
	totalEvidence := 0
	ordered := make([]CheckSnapshot, 0, len(in.Checks))
	digests := make([][]string, 0, len(in.Checks))
	for _, requirementCheck := range doc.Spec.Checks {
		check, ok := provided[requirementCheck.ID]
		if !ok {
			return EvaluatedSnapshot{}, ErrInvalidSubmission
		}
		if check.Verdict == "BLOCKED" {
			result = "BLOCKED"
		} else if check.Verdict == "UNKNOWN" && result == "PASS" {
			result = "UNKNOWN"
		}
		requirements := make(map[string]spec.EvidenceRequirement, len(requirementCheck.EvidenceRequirements))
		for _, requirement := range requirementCheck.EvidenceRequirements {
			requirements[requirement.ID] = requirement
		}
		byRequirement := make(map[string][]Observation)
		seen := make(map[string]bool)
		for _, evidence := range check.Evidence {
			totalEvidence++
			if totalEvidence > MaxEvidenceCount || !validID(evidence.RequirementID) || !validText(evidence.QuerySummary, MaxSummaryRunes) || !validText(evidence.ResultSummary, MaxSummaryRunes) {
				return EvaluatedSnapshot{}, ErrInvalidSubmission
			}
			if evidence.Reference != nil && !validText(*evidence.Reference, MaxSummaryRunes) {
				return EvaluatedSnapshot{}, ErrInvalidSubmission
			}
			observed, err := time.Parse(time.RFC3339Nano, evidence.ObservedAt)
			if err != nil || observed.IsZero() {
				return EvaluatedSnapshot{}, ErrInvalidSubmission
			}
			evidence.ObservedAt = observed.UTC().Format(time.RFC3339Nano)
			req, known := requirements[evidence.RequirementID]
			if !known || !appliesTo(req, check.Verdict) {
				return EvaluatedSnapshot{}, ErrInvalidSubmission
			}
			if containsSensitive(evidence.QuerySummary) || containsSensitive(evidence.ResultSummary) || (evidence.Reference != nil && containsSensitive(*evidence.Reference)) {
				return EvaluatedSnapshot{}, ErrSensitiveEvidence
			}
			canonical, err := json.Marshal(evidence)
			if err != nil || len(canonical) > MaxStructuredEvidenceBytes {
				return EvaluatedSnapshot{}, ErrInvalidSubmission
			}
			sum := sha256.Sum256(canonical)
			identity := evidence.RequirementID + ":" + hex.EncodeToString(sum[:])
			if seen[identity] {
				return EvaluatedSnapshot{}, ErrInvalidSubmission
			}
			seen[identity] = true
			byRequirement[evidence.RequirementID] = append(byRequirement[evidence.RequirementID], evidence)
		}
		normalized := CheckSnapshot{CheckID: check.CheckID, Verdict: check.Verdict, Summary: check.Summary, Evidence: make([]Observation, 0, len(check.Evidence))}
		checkDigests := make([]string, 0, len(check.Evidence))
		for _, requirement := range requirementCheck.EvidenceRequirements {
			group := byRequirement[requirement.ID]
			if appliesTo(requirement, check.Verdict) != (len(group) > 0) {
				return EvaluatedSnapshot{}, ErrInvalidSubmission
			}
			for _, evidence := range group {
				canonical, _ := json.Marshal(evidence)
				hash := sha256.Sum256(canonical)
				normalized.Evidence = append(normalized.Evidence, evidence)
				checkDigests = append(checkDigests, hex.EncodeToString(hash[:]))
			}
		}
		ordered = append(ordered, normalized)
		digests = append(digests, checkDigests)
	}
	if result != in.Verdict {
		return EvaluatedSnapshot{}, ErrVerdictMismatch
	}
	in.Checks = ordered
	payload, err := json.Marshal(in)
	if err != nil || len(payload) > MaxSubmissionBytes {
		return EvaluatedSnapshot{}, ErrInvalidSubmission
	}
	hash := sha256.Sum256(payload)
	decision := "DENY"
	if result == "PASS" {
		decision = "ALLOW"
	}
	return EvaluatedSnapshot{Snapshot: in, CanonicalJSON: string(payload), RequestDigest: hex.EncodeToString(hash[:]), Verdict: result, Decision: decision, EvidenceDigests: digests}, nil
}

func validText(value string, limit int) bool {
	return utf8.ValidString(value) && strings.TrimSpace(value) != "" && utf8.RuneCountInString(value) <= limit
}
func validID(value string) bool {
	return validText(value, MaxIdentifierRunes)
}
