package storage

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	runapp "github.com/alibaba/open-guardrail/internal/application/run"
	"github.com/alibaba/open-guardrail/internal/application/spec"
	rundomain "github.com/alibaba/open-guardrail/internal/domain/run"
)

func TestRunDeadlineAfterDatabaseWait(t *testing.T) {
	for _, backend := range registeredDrivers() {
		t.Run(backend.name, func(t *testing.T) {
			for _, operation := range []string{"submit", "rotate"} {
				t.Run(operation, func(t *testing.T) {
					store := identityStore(t, backend.name)
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					for _, query := range []string{"INSERT INTO workspace(workspace_id,name) VALUES('w','w')", "INSERT INTO project(project_id,workspace_id,name) VALUES('p',(SELECT id FROM workspace WHERE workspace_id='w'),'p')"} {
						if _, err := store.db.ExecContext(ctx, query); err != nil {
							t.Fatal(err)
						}
					}
					scope := spec.Scope{WorkspaceID: "w", ProjectID: "p", ActorID: "admin"}
					if _, _, err := spec.New(store.Specs()).InitializeScene(ctx, scope, "recovery", "Recovery", "default", sampleSpec, "init"); err != nil {
						t.Fatal(err)
					}
					if _, err := store.Specs().SetSceneTTL(ctx, scope, "recovery", 60); err != nil {
						t.Fatal(err)
					}
					runs := store.Runs()
					digest := strings.Repeat("a", 64)
					runScope := runapp.Scope{WorkspaceID: "w", ProjectID: "p", ActorID: "agent", SubmissionCredentialHash: digest}
					created, _, err := runs.CreatePolicyRun(ctx, runScope, runapp.PolicyRunCreateInput{
						SceneKey: "recovery", ChangeSetJSON: "{}", ChangeSetDigest: digest, RequestDigest: digest,
						IdempotencyKey: "create", SubmissionTokenHash: runScope.SubmissionCredentialHash, Now: 1000,
					})
					if err != nil {
						t.Fatal(err)
					}
					// Occupy the pool so the command must wait after entering storage.
					store.db.SetMaxOpenConns(1)
					held, err := store.db.Conn(ctx)
					if err != nil {
						t.Fatal(err)
					}
					defer held.Close()
					waits := store.db.Stats().WaitCount
					var now atomic.Int64
					now.Store(1059)
					result := make(chan error, 1)
					go func() {
						if operation == "rotate" {
							_, err := runs.RotatePolicySubmissionCredential(ctx, runScope, created.ID, strings.Repeat("b", 64), now.Load)
							result <- err
							return
						}
						snapshot := rundomain.SubmissionSnapshot{BaseSubmissionNo: 0, FinishRun: true, Verdict: "PASS", Summary: "healthy", Checks: []rundomain.CheckSnapshot{{CheckID: "readiness", Verdict: "PASS", Summary: "healthy", Evidence: []rundomain.Observation{{RequirementID: "health", ObservedAt: "2026-10-26T00:00:00Z", QuerySummary: "health check", ResultSummary: "healthy"}}}}}
						_, _, err := runs.SubmitSnapshot(ctx, runScope, created.ID, runapp.SnapshotSubmitInput{IdempotencyKey: "submit", Snapshot: snapshot, Now: now.Load})
						result <- err
					}()
					for store.db.Stats().WaitCount == waits {
						select {
						case <-ctx.Done():
							t.Fatal("command did not wait for the occupied database connection")
						case <-time.After(time.Millisecond):
						}
					}
					now.Store(created.DeadlineAt)
					if err := held.Close(); err != nil {
						t.Fatal(err)
					}
					if err := <-result; !errors.Is(err, runapp.ErrExpired) {
						t.Fatalf("command accepted after deadline: %v", err)
					}
					current, err := runs.GetPolicyRun(ctx, runScope, created.ID)
					if err != nil || current.Status != "EXPIRED" || current.Decision == nil || *current.Decision != "DENY" || current.LatestSubmissionNo != 0 {
						t.Fatalf("post-wait Run=%+v err=%v", current, err)
					}
					var retainedHash string
					if err := store.db.QueryRowContext(ctx, "SELECT token_hash FROM run_submission_credential WHERE run_id=(SELECT id FROM guardrail_run WHERE run_id=$1)", created.ID).Scan(&retainedHash); err != nil || retainedHash != runScope.SubmissionCredentialHash {
						t.Fatalf("expired command replaced the credential: %v", err)
					}
				})
			}
		})
	}
}
