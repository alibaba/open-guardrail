package run

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alibaba/open-guardrail/internal/application/spec"
	domain "github.com/alibaba/open-guardrail/internal/domain/run"
)

type reportRepository struct {
	Repository
	current  PolicyRun
	revision spec.PolicyRevision
	final    StoredSubmission
	before   int64
	err      error
}

func (r *reportRepository) RefreshPolicyRun(context.Context, Scope, string, int64) (PolicyRun, error) {
	return r.current, r.err
}
func (r *reportRepository) GetRunPolicyRevision(context.Context, Scope, string) (spec.PolicyRevision, error) {
	return r.revision, nil
}
func (r *reportRepository) ListRunSubmissions(_ context.Context, _ Scope, _ string, _ int, before int64) (SubmissionPage, error) {
	r.before = before
	n := int64(3)
	return SubmissionPage{Items: []SnapshotSubmission{r.final.SnapshotSubmission}, NextBefore: &n}, nil
}
func (r *reportRepository) GetRunSubmission(context.Context, Scope, string, string) (StoredSubmission, error) {
	return r.final, nil
}
func TestReportCapturesBoundedDiagnosticsWithoutBusinessText(t *testing.T) {
	final, digest, decision, result, expires := "sub_final", "digest", "DENY", "BLOCKED", int64(110)
	repo := &reportRepository{current: PolicyRun{ID: "run_test", SceneKey: "recovery", PolicyID: "pol_test", PolicyRevisionID: "rev_test", SpecDigest: digest, Status: "FINISHED", LatestSubmissionNo: 25, FinalSubmissionID: &final, Decision: &decision, Result: &result, DecisionExpiresAt: &expires, ChangeSetJSON: json.RawMessage(`{"private":"business-secret"}`)}, revision: spec.PolicyRevision{ID: "rev_test", PolicyID: "pol_test", Status: "PUBLISHED", Digest: &digest}}
	ref := "private-url"
	repo.final = StoredSubmission{SnapshotSubmission: SnapshotSubmission{ID: final, RunID: "run_test", SubmissionNo: 25, Verdict: "BLOCKED", FinishRun: true, Summary: "private-summary"}, Snapshot: domain.SubmissionSnapshot{Summary: "private-summary", Checks: []domain.CheckSnapshot{{CheckID: "readiness", Verdict: "BLOCKED", Summary: "private-check", Evidence: []domain.Observation{{RequirementID: "health", ObservedAt: "2026-11-12T00:00:00Z", QuerySummary: "private-query", ResultSummary: "private-result", Reference: &ref}}}}}}
	service := New(repo)
	service.now = func() time.Time { return time.Unix(110, 0) }
	report, err := service.Report(context.Background(), Scope{}, "run_test")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(report)
	for _, secret := range []string{"business-secret", "private-", "changeSet\"", "yaml", "summary"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("report contains %q", secret)
		}
	}
	if repo.before != 26 || report.NextBefore == nil || report.FinalSubmission == nil || len(report.FinalSubmission.SnapshotDigest) != 64 || len(report.FinalSubmission.Checks[0].Evidence[0].ResultDigest) != 64 || strings.Join(report.Reasons, ",") != "DECISION_EXPIRED,CHECK_BLOCKED" {
		t.Fatalf("report=%+v", report)
	}
	repo.revision.ID = "rev_other"
	if _, err := service.Report(context.Background(), Scope{}, "run_test"); !errors.Is(err, ErrUnavailable) {
		t.Fatal("inconsistent revision accepted")
	}
	repo.revision.ID = "rev_test"
	for _, status := range []string{"OPEN", "EXPIRED"} {
		repo.current.Status = status
		repo.current.FinalSubmissionID = nil
		r, err := service.Report(context.Background(), Scope{}, "run_test")
		if err != nil || strings.Join(r.Reasons, ",") != "RUN_"+status {
			t.Fatalf("state=%s %+v %v", status, r, err)
		}
	}
	repo.err = ErrNotFound
	if _, err := service.Report(context.Background(), Scope{}, "run_test"); !errors.Is(err, ErrNotFound) {
		t.Fatal("repository error swallowed")
	}
}
