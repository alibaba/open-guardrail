package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	runapp "github.com/alibaba/open-guardrail/internal/application/run"
	"github.com/alibaba/open-guardrail/internal/application/spec"
	rundomain "github.com/alibaba/open-guardrail/internal/domain/run"
)

// Exercise database transactions, rather than only concurrent Go memory access.
func TestModelConcurrentIdempotency(t *testing.T) {
	for _, backend := range registeredDrivers() {
		t.Run(backend.name, func(t *testing.T) {
			store := identityStore(t, backend.name)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			for _, query := range []string{"INSERT INTO workspace(workspace_id,name) VALUES('w','w')", "INSERT INTO project(project_id,workspace_id,name) VALUES('p',(SELECT id FROM workspace WHERE workspace_id='w'),'p')"} {
				if _, err := store.db.ExecContext(ctx, query); err != nil {
					t.Fatal(err)
				}
			}
			scope := spec.Scope{WorkspaceID: "w", ProjectID: "p", ActorID: "admin"}
			burst := func(call func() error) {
				t.Helper()
				start := make(chan struct{})
				results := make(chan error, 8)
				var workers sync.WaitGroup
				for i := 0; i < 8; i++ {
					workers.Add(1)
					go func() { defer workers.Done(); <-start; results <- call() }()
				}
				close(start)
				workers.Wait()
				close(results)
				for err := range results {
					if err != nil {
						t.Fatal(err)
					}
				}
			}
			burst(func() error {
				_, _, err := spec.New(store.Specs()).InitializeScene(ctx, scope, "recovery", "Recovery", "Default", sampleSpec, "init")
				return err
			})
			burst(func() error {
				_, _, err := store.Specs().CreatePolicy(ctx, scope, "recovery", "Other", "create")
				return err
			})
			policy, _, err := store.Specs().CreatePolicy(ctx, scope, "recovery", "Other", "create")
			if err != nil {
				t.Fatal(err)
			}
			burst(func() error {
				_, _, err := spec.New(store.Specs()).PublishPolicyRevision(ctx, scope, policy.ID, sampleSpec, "publish", false)
				return err
			})
			history, err := store.Specs().ListPolicyRevisions(ctx, scope, policy.ID, 100, 0)
			if err != nil || len(history.Items) != 1 {
				t.Fatalf("history=%v err=%v", history, err)
			}
			if _, _, err := store.Specs().CreatePolicy(ctx, scope, "recovery", "Different", "create"); !errors.Is(err, spec.ErrConflict) {
				t.Fatalf("conflicting create: %v", err)
			}
			runScope := runapp.Scope{WorkspaceID: "w", ProjectID: "p", ActorID: "agent"}
			digest := strings.Repeat("a", 64)
			createInput := runapp.PolicyRunCreateInput{
				SceneKey:        "recovery",
				ChangeSetJSON:   "{}",
				ChangeSetDigest: digest,
				RequestDigest:   digest,
				IdempotencyKey:  "concurrent-create",
				Now:             1000,
			}
			type createOutcome struct {
				runID     string
				created   bool
				tokenHash string
			}
			outcomes := make(chan createOutcome, 8)
			var candidate atomic.Int64
			burst(func() error {
				input := createInput
				// A retry proposes a fresh credential, but must retain the winner's.
				input.SubmissionTokenHash = fmt.Sprintf("%064d", 100+candidate.Add(1))
				run, created, err := store.Runs().CreatePolicyRun(ctx, runScope, input)
				if err == nil {
					outcomes <- createOutcome{run.ID, created, input.SubmissionTokenHash}
				}
				return err
			})
			close(outcomes)
			var runID, winningHash string
			createdCount := 0
			for outcome := range outcomes {
				if runID == "" {
					runID = outcome.runID
				}
				if outcome.runID != runID {
					t.Fatalf("same key returned different Runs: %s / %s", runID, outcome.runID)
				}
				if outcome.created {
					createdCount++
					winningHash = outcome.tokenHash
				}
			}
			if createdCount != 1 {
				t.Fatalf("created %d Runs, want exactly one", createdCount)
			}
			var runCount, credentialCount int
			if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM guardrail_run WHERE workspace_id=(SELECT id FROM workspace WHERE workspace_id=$1) AND project_id=(SELECT id FROM project WHERE project_id=$2) AND idempotency_key=$3", runScope.WorkspaceID, runScope.ProjectID, createInput.IdempotencyKey).Scan(&runCount); err != nil {
				t.Fatal(err)
			}
			if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM run_submission_credential WHERE run_id=(SELECT id FROM guardrail_run WHERE run_id=$1)", runID).Scan(&credentialCount); err != nil {
				t.Fatal(err)
			}
			var storedHash string
			if err := store.db.QueryRowContext(ctx, "SELECT token_hash FROM run_submission_credential WHERE run_id=(SELECT id FROM guardrail_run WHERE run_id=$1)", runID).Scan(&storedHash); err != nil {
				t.Fatal(err)
			}
			if runCount != 1 || credentialCount != 1 || storedHash != winningHash {
				t.Fatalf("replay changed stored identity: Runs=%d credentials=%d retained winner=%v", runCount, credentialCount, storedHash == winningHash)
			}
			conflict := createInput
			conflict.RequestDigest = strings.Repeat("b", 64)
			if _, _, err := store.Runs().CreatePolicyRun(ctx, runScope, conflict); !errors.Is(err, runapp.ErrIdempotencyConflict) {
				t.Fatalf("conflicting create replay: %v", err)
			}
			for i := 0; i < 10; i++ {
				tokenHash := fmt.Sprintf("%064d", i)
				run, _, err := store.Runs().CreatePolicyRun(ctx, runScope, runapp.PolicyRunCreateInput{SceneKey: "recovery", ChangeSetJSON: "{}", ChangeSetDigest: digest, RequestDigest: digest, SubmissionTokenHash: tokenHash, IdempotencyKey: fmt.Sprint("run-", i), Now: 1000})
				if err != nil {
					t.Fatal(err)
				}
				submitScope := runScope
				submitScope.SubmissionCredentialHash = tokenHash
				input := runapp.SnapshotSubmitInput{IdempotencyKey: "finish", Now: fixedRunTime(1001), Snapshot: rundomain.SubmissionSnapshot{BaseSubmissionNo: 0, FinishRun: true, Verdict: "PASS", Summary: "healthy", Checks: []rundomain.CheckSnapshot{{CheckID: "readiness", Verdict: "PASS", Summary: "healthy", Evidence: []rundomain.Observation{{RequirementID: "health", ObservedAt: "2026-10-26T00:00:00Z", QuerySummary: "health check", ResultSummary: "healthy"}}}}}}
				burst(func() error { _, _, err := store.Runs().SubmitSnapshot(ctx, submitScope, run.ID, input); return err })
				accepted, _, err := store.Runs().SubmitSnapshot(ctx, submitScope, run.ID, input)
				if err != nil {
					t.Fatal(err)
				}
				outsider := runScope
				outsider.ProjectID = "other"
				if _, err := store.Runs().GetRunPolicyRevision(ctx, outsider, run.ID); !errors.Is(err, runapp.ErrNotFound) {
					t.Fatalf("Revision crossed project scope: %v", err)
				}
				if _, err := store.Runs().GetRunSubmission(ctx, outsider, run.ID, accepted.ID); !errors.Is(err, runapp.ErrNotFound) {
					t.Fatalf("Submission crossed project scope: %v", err)
				}
				if _, err := store.Runs().GetRunSubmission(ctx, runScope, "different-run", accepted.ID); !errors.Is(err, runapp.ErrNotFound) {
					t.Fatalf("Submission crossed Run scope: %v", err)
				}
				input.Snapshot.Summary = "different payload"
				if _, _, err := store.Runs().SubmitSnapshot(ctx, submitScope, run.ID, input); !errors.Is(err, runapp.ErrIdempotencyConflict) {
					t.Fatalf("terminal conflicting replay: %v", err)
				}
				current, err := store.Runs().GetPolicyRun(ctx, runScope, run.ID)
				if err != nil || current.LatestSubmissionNo != 1 || current.Status != "FINISHED" {
					t.Fatalf("run=%+v err=%v", current, err)
				}
			}
		})
	}
}

func TestModelStorageErrorsKeepCause(t *testing.T) {
	cause := fmt.Errorf("driver failure")
	for _, test := range []struct{ err, kind error }{{specError(cause), spec.ErrUnavailable}, {runError(cause), runapp.ErrUnavailable}} {
		if !errors.Is(test.err, cause) || !errors.Is(test.err, test.kind) {
			t.Fatalf("lost error identity: %v", test.err)
		}
	}
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		if !errors.Is(specError(cause), cause) || !errors.Is(runError(cause), cause) {
			t.Fatal("lost context cancellation")
		}
	}
}
