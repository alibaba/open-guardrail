package storage

import (
	"context"
	"errors"
	"strings"
	"testing"

	runapp "github.com/alibaba/open-guardrail/internal/application/run"
	rundomain "github.com/alibaba/open-guardrail/internal/domain/run"
	specdomain "github.com/alibaba/open-guardrail/internal/domain/spec"
)

func TestPolicyRunSubmissionTransactions(t *testing.T) {
	for _, backend := range registeredDrivers() {
		t.Run(backend.name, func(t *testing.T) { testPolicyRunSubmissionTransactionsBackend(t, identityStore(t, backend.name)) })
	}
}
func testPolicyRunSubmissionTransactionsBackend(t *testing.T, store *Store) {
	ctx := context.Background()
	scope := runapp.Scope{WorkspaceID: "w", ProjectID: "p", ActorID: "agent"}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := store.db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	digest := strings.Repeat("a", 64)
	canonical, specDigest, err := specdomain.Validate(sampleSpec)
	if err != nil {
		t.Fatal(err)
	}
	exec("INSERT INTO workspace(workspace_id,name) VALUES('w','w')")
	exec("INSERT INTO project(project_id,workspace_id,name) VALUES('p',(SELECT id FROM workspace WHERE workspace_id='w'),'p')")
	exec("INSERT INTO scene(workspace_id,project_id,scene_key,name) VALUES((SELECT id FROM workspace WHERE workspace_id='w'),(SELECT id FROM project WHERE project_id='p'),'recovery','Recovery')")
	exec("INSERT INTO policy(policy_id,workspace_id,project_id,scene_key,name) VALUES('pol-1',(SELECT id FROM workspace WHERE workspace_id='w'),(SELECT id FROM project WHERE project_id='p'),'recovery','default')")
	exec("INSERT INTO policy_revision(policy_revision_id,policy_id,revision_number,yaml,canonical_json,digest,published_at,published_by,status,updated_by,updated_at) VALUES('rev-1',(SELECT id FROM policy WHERE policy_id='pol-1'),1,$1,$2,$3,1000,'admin','PUBLISHED','admin',1000)", sampleSpec, canonical, specDigest)
	exec("UPDATE policy SET active_revision_id=(SELECT id FROM policy_revision WHERE policy_revision_id='rev-1') WHERE policy_id='pol-1'")
	exec("UPDATE scene SET default_policy_id=(SELECT id FROM policy WHERE policy_id='pol-1') WHERE scene_key='recovery'")
	in := runapp.PolicyRunCreateInput{SceneKey: "recovery", ChangeSetJSON: "{}", ChangeSetDigest: digest, IdempotencyKey: "create-1", RequestDigest: digest, SubmissionTokenHash: digest, Now: 1000}
	run, created, err := store.Runs().CreatePolicyRun(ctx, scope, in)
	if err != nil || !created || run.PolicyRevisionID != "rev-1" || run.DeadlineAt != 87400 {
		t.Fatalf("run=%+v created=%v err=%v", run, created, err)
	}
	explicit := in
	explicit.PolicyRevisionID = "rev-1"
	explicit.CanSelectRevision = true
	explicit.IdempotencyKey = "create-explicit"
	explicit.SubmissionTokenHash = strings.Repeat("b", 64)
	selected, selectedCreated, err := store.Runs().CreatePolicyRun(ctx, scope, explicit)
	if err != nil || !selectedCreated || selected.PolicyRevisionID != "rev-1" {
		t.Fatalf("explicit revision run=%+v created=%v err=%v", selected, selectedCreated, err)
	}
	replay, created, err := store.Runs().CreatePolicyRun(ctx, scope, in)
	if err != nil || created || replay.ID != run.ID {
		t.Fatalf("replay=%+v created=%v err=%v", replay, created, err)
	}
	changed := in
	changed.RequestDigest = strings.Repeat("b", 64)
	if _, _, err := store.Runs().CreatePolicyRun(ctx, scope, changed); !errors.Is(err, runapp.ErrIdempotencyConflict) {
		t.Fatalf("conflicting create: %v", err)
	}
	submissionScope := scope
	submissionScope.SubmissionCredentialHash = digest
	snapshot := rundomain.SubmissionSnapshot{BaseSubmissionNo: 0, FinishRun: false, Verdict: "PASS", Summary: "healthy", Checks: []rundomain.CheckSnapshot{{CheckID: "readiness", Verdict: "PASS", Summary: "healthy", Evidence: []rundomain.Observation{{RequirementID: "health", ObservedAt: "2026-10-26T00:00:00Z", QuerySummary: "checked app health", ResultSummary: "healthy"}, {RequirementID: "health", ObservedAt: "2026-10-26T00:01:00Z", QuerySummary: "checked host health", ResultSummary: "healthy"}}}}}
	first, created, err := store.Runs().SubmitSnapshot(ctx, submissionScope, run.ID, runapp.SnapshotSubmitInput{IdempotencyKey: "submit-1", Snapshot: snapshot, Now: fixedRunTime(1001)})
	if err != nil || !created || first.SubmissionNo != 1 || first.FinishRun {
		t.Fatalf("first=%+v created=%v err=%v", first, created, err)
	}
	current, err := store.Runs().GetPolicyRun(ctx, scope, run.ID)
	if err != nil || current.Status != "OPEN" || current.LatestSubmissionNo != 1 {
		t.Fatalf("current=%+v err=%v", current, err)
	}
	stale := snapshot
	stale.Summary = "new observation"
	if _, _, err := store.Runs().SubmitSnapshot(ctx, submissionScope, run.ID, runapp.SnapshotSubmitInput{IdempotencyKey: "submit-stale", Snapshot: stale, Now: fixedRunTime(1002)}); !errors.Is(err, runapp.ErrBaseSubmissionMismatch) {
		t.Fatalf("stale submit: %v", err)
	}
	snapshot.BaseSubmissionNo = 1
	snapshot.FinishRun = true
	final, created, err := store.Runs().SubmitSnapshot(ctx, submissionScope, run.ID, runapp.SnapshotSubmitInput{IdempotencyKey: "submit-2", Snapshot: snapshot, Now: fixedRunTime(1003)})
	if err != nil || !created || final.SubmissionNo != 2 || !final.FinishRun {
		t.Fatalf("final=%+v created=%v err=%v", final, created, err)
	}
	current, err = store.Runs().GetPolicyRun(ctx, scope, run.ID)
	if err != nil || current.Status != "FINISHED" || current.LatestSubmissionNo != 2 || current.FinalSubmissionID == nil || *current.FinalSubmissionID != final.ID {
		t.Fatalf("finished=%+v err=%v", current, err)
	}

	report, err := runapp.New(store.Runs()).Report(ctx, scope, run.ID)
	if err != nil || report.FinalSubmission == nil || report.FinalSubmission.ID != final.ID || report.Run.FinalSubmissionID == nil || report.Revision.Digest != specDigest || len(report.FinalSubmission.Checks) != 1 || len(report.FinalSubmission.Checks[0].Evidence) != 2 {
		t.Fatalf("Run report failed: %+v %v", report, err)
	}
	if _, err := runapp.New(store.Runs()).Report(ctx, runapp.Scope{WorkspaceID: "w", ProjectID: "other"}, run.ID); !errors.Is(err, runapp.ErrNotFound) {
		t.Fatalf("cross-project report: %v", err)
	}
	stillFinished, err := store.Runs().RefreshPolicyRun(ctx, scope, run.ID, run.DeadlineAt+1000)
	if err != nil || stillFinished.Status != "FINISHED" || stillFinished.Decision == nil || *stillFinished.Decision != "ALLOW" {
		t.Fatalf("finished Run changed after deadline: %+v %v", stillFinished, err)
	}
	history, err := store.Runs().ListRunSubmissions(ctx, scope, run.ID, 1, 0)
	if err != nil || len(history.Items) != 1 || history.Items[0].ID != final.ID || history.NextBefore == nil || *history.NextBefore != 2 {
		t.Fatalf("history=%+v err=%v", history, err)
	}
	older, err := store.Runs().ListRunSubmissions(ctx, scope, run.ID, 1, *history.NextBefore)
	if err != nil || len(older.Items) != 1 || older.Items[0].ID != first.ID || older.NextBefore != nil {
		t.Fatalf("older history=%+v err=%v", older, err)
	}
	for _, denied := range []runapp.Scope{{WorkspaceID: "other", ProjectID: "p"}, {WorkspaceID: "w", ProjectID: "other"}} {
		if _, err := store.Runs().ListRunSubmissions(ctx, denied, run.ID, 20, 0); !errors.Is(err, runapp.ErrNotFound) {
			t.Fatalf("cross-scope history err=%v", err)
		}
	}
	emptyHistory, err := store.Runs().ListRunSubmissions(ctx, scope, selected.ID, 20, 0)
	if err != nil || len(emptyHistory.Items) != 0 || emptyHistory.Items == nil {
		t.Fatalf("empty history=%+v err=%v", emptyHistory, err)
	}
	repeated, created, err := store.Runs().SubmitSnapshot(ctx, submissionScope, run.ID, runapp.SnapshotSubmitInput{IdempotencyKey: "submit-2", Snapshot: snapshot, Now: fixedRunTime(1004)})
	if err != nil || created || repeated.ID != final.ID {
		t.Fatalf("replayed=%+v created=%v err=%v", repeated, created, err)
	}
	if _, _, err := store.Runs().SubmitSnapshot(ctx, submissionScope, run.ID, runapp.SnapshotSubmitInput{IdempotencyKey: "submit-3", Snapshot: snapshot, Now: fixedRunTime(1005)}); !errors.Is(err, runapp.ErrCompleted) {
		t.Fatalf("new submission after finish: %v", err)
	}
	var result, decision string
	if err := store.db.QueryRowContext(ctx, "SELECT result_verdict,decision_verdict FROM guardrail_run WHERE run_id=$1", run.ID).Scan(&result, &decision); err != nil || result != "PASS" || decision != "ALLOW" {
		t.Fatalf("result=%s decision=%s err=%v", result, decision, err)
	}
	page, more, err := store.Runs().ListPolicyRuns(ctx, scope, "recovery", "FINISHED", 10, 0, "", 1005)
	if err != nil || more || len(page) != 1 || page[0].ID != run.ID || page[0].Decision == nil || *page[0].Decision != "ALLOW" {
		t.Fatalf("finished page=%+v more=%v err=%v", page, more, err)
	}
}
