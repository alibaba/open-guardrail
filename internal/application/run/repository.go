package run

import (
	"context"

	"github.com/alibaba/open-guardrail/internal/application/spec"
)

// Repository defines the persistence boundary independently of the SQL adapter.
type Repository interface {
	GetPolicyRun(ctx context.Context, scope Scope, id string) (PolicyRun, error)
	CreatePolicyRun(ctx context.Context, scope Scope, input PolicyRunCreateInput) (PolicyRun, bool, error)
	SubmitSnapshot(ctx context.Context, scope Scope, runID string, input SnapshotSubmitInput) (SnapshotSubmission, bool, error)
	RefreshPolicyRun(ctx context.Context, scope Scope, id string, now int64) (PolicyRun, error)
	RotatePolicySubmissionCredential(ctx context.Context, scope Scope, id, hash string, now func() int64) (PolicyRun, error)
	ListPolicyRuns(ctx context.Context, scope Scope, sceneKey, status string, limit int, cursorAt int64, cursorID string, now int64) ([]PolicyRunSummary, bool, error)
	GetRunPolicyRevision(ctx context.Context, scope Scope, runID string) (spec.PolicyRevision, error)
	GetRunSubmission(ctx context.Context, scope Scope, runID, submissionID string) (StoredSubmission, error)
	ListRunSubmissions(ctx context.Context, scope Scope, runID string, limit int, before int64) (SubmissionPage, error)
	AuthenticateSubmissionToken(ctx context.Context, runID, tokenHash string) (Scope, error)
}
