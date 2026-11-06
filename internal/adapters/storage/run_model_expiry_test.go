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

func TestPolicyRunExpiryAndCredentialRotation(t *testing.T) {
	for _, backend := range registeredDrivers() {
		t.Run(backend.name, func(t *testing.T) { testPolicyRunExpiryAndCredentialRotationBackend(t, identityStore(t, backend.name)) })
	}
}
func testPolicyRunExpiryAndCredentialRotationBackend(t *testing.T, store *Store) {
	ctx := context.Background()
	scope := runapp.Scope{WorkspaceID: "w", ProjectID: "p", ActorID: "orchestrator"}
	canonical, specDigest, err := specdomain.Validate(sampleSpec)
	if err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := store.db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO workspace(workspace_id,name) VALUES('w','w')")
	exec("INSERT INTO project(project_id,workspace_id,name) VALUES('p',(SELECT id FROM workspace WHERE workspace_id='w'),'p')")
	exec("INSERT INTO scene(workspace_id,project_id,scene_key,name,run_ttl_seconds) VALUES((SELECT id FROM workspace WHERE workspace_id='w'),(SELECT id FROM project WHERE project_id='p'),'recovery','Recovery',60)")
	exec("INSERT INTO policy(policy_id,workspace_id,project_id,scene_key,name) VALUES('pol',(SELECT id FROM workspace WHERE workspace_id='w'),(SELECT id FROM project WHERE project_id='p'),'recovery','default')")
	exec("INSERT INTO policy_revision(policy_revision_id,policy_id,revision_number,yaml,canonical_json,digest,published_at,published_by,status,updated_by,updated_at) VALUES('rev',(SELECT id FROM policy WHERE policy_id='pol'),1,$1,$2,$3,1,'admin','PUBLISHED','admin',1)", sampleSpec, canonical, specDigest)
	exec("UPDATE policy SET active_revision_id=(SELECT id FROM policy_revision WHERE policy_revision_id='rev') WHERE policy_id='pol'")
	exec("UPDATE scene SET default_policy_id=(SELECT id FROM policy WHERE policy_id='pol') WHERE scene_key='recovery'")
	digest := strings.Repeat("a", 64)
	in := runapp.PolicyRunCreateInput{SceneKey: "recovery", ChangeSetJSON: "{}", ChangeSetDigest: digest, IdempotencyKey: "create", RequestDigest: digest, SubmissionTokenHash: digest, Now: 1000}
	run, _, err := store.Runs().CreatePolicyRun(ctx, scope, in)
	if err != nil {
		t.Fatal(err)
	}
	staleSubmissionScope, err := store.Runs().AuthenticateSubmissionToken(ctx, run.ID, digest)
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := store.Runs().RotatePolicySubmissionCredential(ctx, scope, run.ID, strings.Repeat("b", 64), fixedRunTime(1050))
	if err != nil || rotated.DeadlineAt != 1060 {
		t.Fatalf("rotate=%+v err=%v", rotated, err)
	}
	snapshot := rundomain.SubmissionSnapshot{BaseSubmissionNo: 0, FinishRun: false, Verdict: "PASS", Summary: "healthy", Checks: []rundomain.CheckSnapshot{{CheckID: "readiness", Verdict: "PASS", Summary: "healthy", Evidence: []rundomain.Observation{{RequirementID: "health", ObservedAt: "2026-10-26T00:00:00Z", QuerySummary: "checked health", ResultSummary: "healthy"}}}}}
	if _, _, err := store.Runs().SubmitSnapshot(ctx, staleSubmissionScope, run.ID, runapp.SnapshotSubmitInput{IdempotencyKey: "stale-credential", Snapshot: snapshot, Now: fixedRunTime(1051)}); !errors.Is(err, runapp.ErrUnauthorized) {
		t.Fatalf("stale credential submitted after rotation: %v", err)
	}
	currentSubmissionScope, err := store.Runs().AuthenticateSubmissionToken(ctx, run.ID, strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	accepted, created, err := store.Runs().SubmitSnapshot(ctx, currentSubmissionScope, run.ID, runapp.SnapshotSubmitInput{IdempotencyKey: "current-credential", Snapshot: snapshot, Now: fixedRunTime(1051)})
	if err != nil || !created {
		t.Fatalf("current credential submit created=%v err=%v", created, err)
	}
	thirdHash := strings.Repeat("c", 64)
	if _, err := store.Runs().RotatePolicySubmissionCredential(ctx, scope, run.ID, thirdHash, fixedRunTime(1052)); err != nil {
		t.Fatalf("second rotation: %v", err)
	}
	if _, _, err := store.Runs().SubmitSnapshot(ctx, currentSubmissionScope, run.ID, runapp.SnapshotSubmitInput{IdempotencyKey: "current-credential", Snapshot: snapshot, Now: fixedRunTime(1053)}); !errors.Is(err, runapp.ErrUnauthorized) {
		t.Fatalf("stale credential replayed after rotation: %v", err)
	}
	thirdScope, err := store.Runs().AuthenticateSubmissionToken(ctx, run.ID, thirdHash)
	if err != nil {
		t.Fatal(err)
	}
	replayed, created, err := store.Runs().SubmitSnapshot(ctx, thirdScope, run.ID, runapp.SnapshotSubmitInput{IdempotencyKey: "current-credential", Snapshot: snapshot, Now: fixedRunTime(1053)})
	if err != nil || created || replayed.ID != accepted.ID {
		t.Fatalf("current credential replay=%+v created=%v err=%v", replayed, created, err)
	}
	if _, err := store.Runs().RotatePolicySubmissionCredential(ctx, scope, run.ID, strings.Repeat("d", 64), fixedRunTime(1060)); !errors.Is(err, runapp.ErrExpired) {
		t.Fatalf("rotate after deadline=%v", err)
	}
	expired, err := store.Runs().RefreshPolicyRun(ctx, scope, run.ID, 1061)
	if err != nil || expired.Status != "EXPIRED" || expired.Decision == nil || *expired.Decision != "DENY" || expired.DecisionExpiresAt == nil || *expired.DecisionExpiresAt != 1960 {
		t.Fatalf("expired=%+v err=%v", expired, err)
	}
	if _, err := store.Runs().RotatePolicySubmissionCredential(ctx, scope, run.ID, strings.Repeat("e", 64), fixedRunTime(1062)); !errors.Is(err, runapp.ErrExpired) {
		t.Fatalf("rotate expired=%v", err)
	}
	// Submission and list expiry use the same transition, including one audit
	// event per Run, regardless of later refreshes.
	in.IdempotencyKey = "submit-expired"
	submitting, _, err := store.Runs().CreatePolicyRun(ctx, scope, in)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.FinishRun = true
	submittingScope := scope
	submittingScope.SubmissionCredentialHash = in.SubmissionTokenHash
	if _, _, err := store.Runs().SubmitSnapshot(ctx, submittingScope, submitting.ID, runapp.SnapshotSubmitInput{IdempotencyKey: "late-submit", Snapshot: snapshot, Now: fixedRunTime(1060)}); !errors.Is(err, runapp.ErrExpired) {
		t.Fatalf("submit expired=%v", err)
	}
	in.IdempotencyKey = "list-expired"
	in.SubmissionTokenHash = strings.Repeat("f", 64)
	listing, _, err := store.Runs().CreatePolicyRun(ctx, scope, in)
	if err != nil {
		t.Fatal(err)
	}
	page, more, err := store.Runs().ListPolicyRuns(ctx, scope, "", "EXPIRED", 100, 0, "", 1061)
	if err != nil || more || len(page) != 3 {
		t.Fatalf("expired page=%+v more=%v err=%v", page, more, err)
	}
	for _, item := range page {
		if item.Status != "EXPIRED" || item.Decision == nil || *item.Decision != "DENY" {
			t.Fatalf("unexpected expired Run: %+v", item)
		}
	}
	// Exercise positional parameter binding with a real nonzero cursor on all backends.
	cursorAt, cursorID := int64(0), ""
	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		items, hasMore, err := store.Runs().ListPolicyRuns(ctx, scope, "recovery", "EXPIRED", 1, cursorAt, cursorID, 1062)
		if err != nil || len(items) != 1 || hasMore != (i < 2) {
			t.Fatalf("cursor page %d: %+v more=%v err=%v", i, items, hasMore, err)
		}
		if seen[items[0].ID] {
			t.Fatal("cursor repeated a Run")
		}
		seen[items[0].ID] = true
		cursorAt, cursorID = items[0].CreatedAt, items[0].ID
	}
	if _, _, err := store.Runs().ListPolicyRuns(ctx, scope, "", "EXPIRED", 100, 0, "", 1062); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{run.ID, submitting.ID, listing.ID} {
		var count int
		if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_event WHERE event_type='run_expired' AND resource_type='run' AND resource_id=$1", id).Scan(&count); err != nil || count != 1 {
			t.Fatalf("Run %s expiry audit count=%d err=%v", id, count, err)
		}
	}
}
