package run

import (
	"encoding/json"
	"errors"

	"github.com/alibaba/open-guardrail/internal/domain/resource"
	rundomain "github.com/alibaba/open-guardrail/internal/domain/run"
)

var (
	ErrBaseSubmissionMismatch = errors.New("base submission number does not match latest")
	ErrVerdictMismatch        = rundomain.ErrVerdictMismatch
)

type PolicyRun struct {
	Selection          *Selection      `json:"selection,omitempty"`
	ID                 string          `json:"id"`
	SceneKey           string          `json:"sceneKey"`
	PolicyID           string          `json:"policyId"`
	PolicyRevisionID   string          `json:"policyRevisionId"`
	SpecDigest         string          `json:"specDigest"`
	Status             string          `json:"status"`
	ChangeSetJSON      json.RawMessage `json:"changeSet"`
	ChangeSetDigest    string          `json:"changeSetDigest"`
	RunTTLSeconds      int64           `json:"runTtlSeconds"`
	DeadlineAt         int64           `json:"deadlineAt"`
	LatestSubmissionNo int64           `json:"latestSubmissionNo"`
	FinalSubmissionID  *string         `json:"finalSubmissionId,omitempty"`
	CreatedAt          int64           `json:"createdAt"`
	FinishedAt         *int64          `json:"finishedAt,omitempty"`
	ExpiredAt          *int64          `json:"expiredAt,omitempty"`
	Result             *string         `json:"result,omitempty"`
	Decision           *string         `json:"decision,omitempty"`
	DecisionExpiresAt  *int64          `json:"decisionExpiresAt,omitempty"`
}

// PolicyRunSummary is the bounded representation returned by list operations.
// The potentially large ChangeSet is available only from the Run detail API.
type PolicyRunSummary struct {
	ID                 string  `json:"id"`
	SceneKey           string  `json:"sceneKey"`
	PolicyID           string  `json:"policyId"`
	PolicyRevisionID   string  `json:"policyRevisionId"`
	SpecDigest         string  `json:"specDigest"`
	Status             string  `json:"status"`
	ChangeSetDigest    string  `json:"changeSetDigest"`
	RunTTLSeconds      int64   `json:"runTtlSeconds"`
	DeadlineAt         int64   `json:"deadlineAt"`
	LatestSubmissionNo int64   `json:"latestSubmissionNo"`
	FinalSubmissionID  *string `json:"finalSubmissionId,omitempty"`
	CreatedAt          int64   `json:"createdAt"`
	FinishedAt         *int64  `json:"finishedAt,omitempty"`
	ExpiredAt          *int64  `json:"expiredAt,omitempty"`
	Result             *string `json:"result,omitempty"`
	Decision           *string `json:"decision,omitempty"`
	DecisionExpiresAt  *int64  `json:"decisionExpiresAt,omitempty"`
}

func (r PolicyRun) Summary() PolicyRunSummary {
	return PolicyRunSummary{
		ID: r.ID, SceneKey: r.SceneKey, PolicyID: r.PolicyID, PolicyRevisionID: r.PolicyRevisionID,
		SpecDigest: r.SpecDigest, Status: r.Status, ChangeSetDigest: r.ChangeSetDigest,
		RunTTLSeconds: r.RunTTLSeconds, DeadlineAt: r.DeadlineAt, LatestSubmissionNo: r.LatestSubmissionNo,
		FinalSubmissionID: r.FinalSubmissionID, CreatedAt: r.CreatedAt, FinishedAt: r.FinishedAt,
		ExpiredAt: r.ExpiredAt, Result: r.Result, Decision: r.Decision, DecisionExpiresAt: r.DecisionExpiresAt,
	}
}

type PolicyRunCreateInput struct {
	Resources           []resource.Reference
	SceneKey            string
	PolicyRevisionID    string // Empty selects bindings or the Scene default active Revision.
	CanSelectRevision   bool   // Granted only by a dedicated machine scope.
	ChangeSetJSON       string
	ChangeSetDigest     string
	IdempotencyKey      string
	RequestDigest       string
	SubmissionTokenHash string
	Now                 int64
}

type SnapshotSubmitInput struct {
	IdempotencyKey string
	Snapshot       rundomain.SubmissionSnapshot
	// Sample only after acquiring the Run lock, immediately before acceptance.
	Now func() int64
}

type SnapshotSubmission struct {
	ID               string `json:"id"`
	RunID            string `json:"runId"`
	SubmissionNo     int64  `json:"submissionNo"`
	BaseSubmissionNo int64  `json:"baseSubmissionNo"`
	Verdict          string `json:"verdict"`
	Summary          string `json:"summary"`
	FinishRun        bool   `json:"finishRun"`
	CreatedAt        int64  `json:"createdAt"`
}

// StoredSubmission exposes a persisted snapshot with its server-assigned identity.
type StoredSubmission struct {
	SnapshotSubmission
	Snapshot rundomain.SubmissionSnapshot `json:"snapshot"`
}

// SubmissionPage lists metadata only; evidence is loaded for one selected snapshot.
type SubmissionPage struct {
	Items      []SnapshotSubmission `json:"items"`
	NextBefore *int64               `json:"nextBefore,omitempty"`
}
