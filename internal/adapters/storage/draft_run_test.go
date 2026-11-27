package storage

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	runapp "github.com/alibaba/open-guardrail/internal/application/run"
	"github.com/alibaba/open-guardrail/internal/application/spec"
	"github.com/alibaba/open-guardrail/internal/domain/resource"
	rundomain "github.com/alibaba/open-guardrail/internal/domain/run"
)

func TestDraftBindingRunKeepsItsSpec(t *testing.T) {
	for _, backend := range registeredDrivers() {
		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()
			store := identityStore(t, backend.name)
			scope := spec.Scope{WorkspaceID: "default", ProjectID: "default", ActorID: "admin"}
			service := spec.New(store.Specs())
			initial, _, err := service.InitializeScene(ctx, scope, "recovery", "Recovery", "Policy", sampleSpec, "init")
			if err != nil {
				t.Fatal(err)
			}
			runScope := runapp.Scope{WorkspaceID: "default", ProjectID: "default", ActorID: "admin"}
			published, created, err := store.Runs().CreatePolicyRun(ctx, runScope, runapp.PolicyRunCreateInput{
				SceneKey: "recovery", ChangeSetJSON: "{}", ChangeSetDigest: strings.Repeat("a", 64),
				RequestDigest: strings.Repeat("b", 64), SubmissionTokenHash: strings.Repeat("e", 64),
				IdempotencyKey: "published", Now: time.Now().Unix(),
			})
			if err != nil || !created {
				t.Fatalf("published Run: %+v created=%v err=%v", published, created, err)
			}
			var unsnapshotted int
			if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM guardrail_run WHERE run_id=$1 AND spec_yaml IS NULL AND spec_canonical_json IS NULL", published.ID).Scan(&unsnapshotted); err != nil || unsnapshotted != 1 {
				t.Fatalf("published Run copied immutable YAML into the limited snapshot column: count=%d err=%v", unsnapshotted, err)
			}
			var canonical, digest string
			if err := store.db.QueryRowContext(ctx, "SELECT canonical_json,digest FROM policy_revision WHERE policy_revision_id=$1", initial.Revision.ID).Scan(&canonical, &digest); err != nil {
				t.Fatal(err)
			}
			legacyYAML := sampleSpec + strings.Repeat(" ", 65536-len(sampleSpec))
			if _, err := store.db.ExecContext(ctx, `INSERT INTO policy_revision(policy_revision_id,policy_id,status,revision_number,yaml,canonical_json,digest,updated_by,updated_at,published_by,published_at)
 SELECT 'rev_legacy64k',id,'PUBLISHED',2,$1,$2,$3,'admin',1,'admin',1 FROM policy WHERE policy_id=$4`, legacyYAML, canonical, digest, initial.Policy.ID); err != nil {
				t.Fatal(err)
			}
			legacy, created, err := store.Runs().CreatePolicyRun(ctx, runapp.Scope{WorkspaceID: "default", ProjectID: "default", ActorID: "admin", CanSelectRevision: true}, runapp.PolicyRunCreateInput{
				SceneKey: "recovery", PolicyRevisionID: "rev_legacy64k", CanSelectRevision: true,
				ChangeSetJSON: "{}", ChangeSetDigest: strings.Repeat("a", 64), RequestDigest: strings.Repeat("b", 64),
				SubmissionTokenHash: strings.Repeat("f", 64), IdempotencyKey: "legacy", Now: time.Now().Unix(),
			})
			if err != nil || !created {
				t.Fatalf("legacy 65,536-byte published Spec could not create Run: %+v created=%v err=%v", legacy, created, err)
			}
			legacyRevision, err := store.Runs().GetRunPolicyRevision(ctx, runScope, legacy.ID)
			if err != nil || legacyRevision.YAML != legacyYAML {
				t.Fatalf("legacy published YAML was not preserved: err=%v", err)
			}
			firstYAML := sampleSpec
			secondYAML := strings.Replace(sampleSpec, "id: readiness", "id: readiness-new", 1)
			draft, err := service.SavePolicyDraft(ctx, scope, initial.Policy.ID, firstYAML)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.db.ExecContext(ctx, "UPDATE policy_revision SET updated_by=$1,updated_at=$2,metadata_json=$3 WHERE policy_revision_id=$4", "editor-a", 11, `{"source":"draft-a"}`, draft.ID); err != nil {
				t.Fatal(err)
			}
			ref := resource.Reference{Kind: "application", ID: "test-app"}
			binding := spec.Binding{Resource: ref, PolicyID: initial.Policy.ID, PolicyRevisionID: &draft.ID}
			if _, err := store.Specs().PutBindingWithOptions(ctx, scope, "recovery", binding, spec.BindingWriteOptions{}); !errors.Is(err, spec.ErrInvalid) {
				t.Fatalf("draft bound without opt-in: %v", err)
			}
			if _, err := store.Specs().PutBindingWithOptions(ctx, scope, "recovery", binding, spec.BindingWriteOptions{AllowDraft: true}); err != nil {
				t.Fatal(err)
			}
			create := func(key string) runapp.PolicyRun {
				t.Helper()
				tokenHash := strings.Repeat("c", 64)
				if key == "second" {
					tokenHash = strings.Repeat("d", 64)
				}
				run, created, err := store.Runs().CreatePolicyRun(ctx, runScope, runapp.PolicyRunCreateInput{
					SceneKey: "recovery", ResourceGroups: [][]resource.Reference{{ref}}, ChangeSetJSON: "{}",
					ChangeSetDigest: strings.Repeat("a", 64), RequestDigest: strings.Repeat("b", 64),
					SubmissionTokenHash: tokenHash, IdempotencyKey: key, Now: time.Now().Unix(),
				})
				if err != nil || !created || run.Selection == nil || run.Selection.RevisionStatus != "DRAFT" {
					t.Fatalf("draft Run: %+v created=%v err=%v", run, created, err)
				}
				return run
			}
			first := create("first")
			if _, err := store.db.ExecContext(ctx, "UPDATE guardrail_run SET `spec_yaml`=$1 WHERE run_id=$2", secondYAML, first.ID); err == nil {
				t.Fatal("quoted frozen YAML assignment was accepted")
			}
			if _, err := store.db.ExecContext(ctx, "UPDATE guardrail_run SET `spec_canonical_json`=$1 WHERE run_id=$2", "{}", first.ID); err == nil {
				t.Fatal("quoted frozen canonical Spec assignment was accepted")
			}
			if _, err := store.db.ExecContext(ctx, "UPDATE guardrail_run SET spec_updated_by=$1 WHERE run_id=$2", "intruder", first.ID); err == nil {
				t.Fatal("frozen draft editor assignment was accepted")
			}
			if _, err := service.SavePolicyDraft(ctx, scope, initial.Policy.ID, secondYAML); err != nil {
				t.Fatal(err)
			}
			if _, err := store.db.ExecContext(ctx, "UPDATE policy_revision SET updated_by=$1,updated_at=$2,metadata_json=$3 WHERE policy_revision_id=$4", "editor-b", 22, `{"source":"draft-b"}`, draft.ID); err != nil {
				t.Fatal(err)
			}
			second := create("second")
			if first.SpecDigest == second.SpecDigest {
				t.Fatal("draft edit did not change the Run snapshot")
			}
			if _, _, err := service.PublishPolicyRevision(ctx, scope, initial.Policy.ID, secondYAML, "publish", true); err != nil {
				t.Fatal(err)
			}
			for _, item := range []struct {
				run       runapp.PolicyRun
				yaml      string
				updatedBy string
				updatedAt int64
				metadata  string
			}{{first, firstYAML, "editor-a", 11, `{"source":"draft-a"}`}, {second, secondYAML, "editor-b", 22, `{"source":"draft-b"}`}} {
				revision, err := store.Runs().GetRunPolicyRevision(ctx, runScope, item.run.ID)
				if err != nil || revision.Status != "DRAFT" || revision.YAML != item.yaml || revision.Digest == nil || *revision.Digest != item.run.SpecDigest || revision.RevisionNumber != nil || revision.UpdatedBy != item.updatedBy || revision.UpdatedAt != item.updatedAt || string(revision.Metadata) != item.metadata {
					t.Fatalf("Run revision changed after publish: %+v %v", revision, err)
				}
				report, err := runapp.New(store.Runs()).Report(ctx, runScope, item.run.ID)
				if err != nil || report.Revision.Status != "DRAFT" || report.Revision.Digest != item.run.SpecDigest {
					t.Fatalf("draft Run report: %+v %v", report, err)
				}
			}
			submissionScope := runScope
			submissionScope.SubmissionCredentialHash = strings.Repeat("c", 64)
			oldCheck := rundomain.SubmissionSnapshot{Verdict: "PASS", Summary: "ok", Checks: []rundomain.CheckSnapshot{{CheckID: "readiness", Verdict: "PASS", Summary: "ok", Evidence: []rundomain.Observation{{RequirementID: "health", ObservedAt: "2026-11-26T00:00:00Z", QuerySummary: "checked", ResultSummary: "ok"}}}}}
			if _, _, err := store.Runs().SubmitSnapshot(ctx, submissionScope, first.ID, runapp.SnapshotSubmitInput{IdempotencyKey: "old", Snapshot: oldCheck, Now: fixedRunTime(time.Now().Unix())}); err != nil {
				t.Fatalf("old Run lost its draft snapshot: %v", err)
			}
			submissionScope.SubmissionCredentialHash = strings.Repeat("d", 64)
			if _, _, err := store.Runs().SubmitSnapshot(ctx, submissionScope, second.ID, runapp.SnapshotSubmitInput{IdempotencyKey: "new", Snapshot: oldCheck, Now: fixedRunTime(time.Now().Unix())}); err == nil {
				t.Fatal("new Run accepted an old draft check")
			}
		})
	}
}
