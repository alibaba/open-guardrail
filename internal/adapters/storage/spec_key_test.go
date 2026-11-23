package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/alibaba/open-guardrail/internal/application/spec"
)

func TestSpecKeyIdentity(t *testing.T) {
	for _, backend := range registeredDrivers() {
		t.Run(backend.name, func(t *testing.T) {
			store := identityStore(t, backend.name)
			ctx := context.Background()
			scope := spec.Scope{WorkspaceID: "default", ProjectID: "default", ActorID: "admin"}
			repo := spec.New(store.Specs())
			initial, _, err := repo.InitializeScene(ctx, scope, "recovery", "Recovery", "Shared label", sampleSpec, "init")
			if err != nil || initial.Policy.SpecKey != "recovery" {
				t.Fatalf("initialize: %+v %v", initial, err)
			}
			for _, name := range []string{"", "Uppercase", "white space", strings.Repeat("a", 129)} {
				if _, _, err := repo.CreatePolicy(ctx, scope, "recovery", "Label", name, "invalid-"+name); !errors.Is(err, spec.ErrInvalid) {
					t.Fatalf("accepted invalid key %q: %v", name, err)
				}
			}
			if _, _, err := repo.CreatePolicy(ctx, scope, "recovery", "Other label", "recovery", "duplicate"); !errors.Is(err, spec.ErrSpecKeyConflict) {
				t.Fatalf("duplicate key: %v", err)
			}
			p, _, err := repo.CreatePolicy(ctx, scope, "recovery", "Shared label", "alternate", "create")
			if err != nil || p.SpecKey != "alternate" {
				t.Fatalf("display labels may repeat: %+v %v", p, err)
			}
			longKey := strings.Repeat("a", 128)
			longPolicy, _, err := repo.CreatePolicy(ctx, scope, "recovery", "Long key", longKey, "long-key")
			if err != nil || longPolicy.SpecKey != longKey {
				t.Fatalf("128-character key: %+v %v", longPolicy, err)
			}
			longYAML := strings.Replace(sampleSpec, "name: recovery", "name: "+longKey, 1)
			if _, _, err := repo.PublishPolicyRevision(ctx, scope, longPolicy.ID, longYAML, "long-revision", false); err != nil {
				t.Fatalf("128-character YAML metadata.name: %v", err)
			}
			if replay, created, err := repo.CreatePolicy(ctx, scope, "recovery", "Shared label", "alternate", "create"); err != nil || created || replay.ID != p.ID {
				t.Fatalf("creation retry: %+v %v", replay, err)
			}
			if _, _, err := repo.CreatePolicy(ctx, scope, "recovery", "Shared label", "changed", "create"); !errors.Is(err, spec.ErrConflict) {
				t.Fatalf("changed key on retry: %v", err)
			}
			if _, _, err := repo.CreatePolicy(ctx, scope, "recovery", "Shared label", "", "create"); !errors.Is(err, spec.ErrConflict) {
				t.Fatalf("new-format retry omitted its key: %v", err)
			}
			for _, raw := range []string{"", "invalid: [", "metadata:\n  name: alternate\nspec: incomplete\n"} {
				if _, err := repo.SavePolicyDraft(ctx, scope, p.ID, raw); err != nil {
					t.Fatalf("incomplete Draft: %v", err)
				}
			}
			if _, err := repo.SavePolicyDraft(ctx, scope, p.ID, "metadata:\n  name: changed\n"); !errors.Is(err, spec.ErrSpecKeyMismatch) {
				t.Fatalf("Draft changed identity: %v", err)
			}
			if _, _, err := repo.PublishPolicyRevision(ctx, scope, p.ID, sampleSpec, "wrong-name", false); !errors.Is(err, spec.ErrSpecKeyMismatch) {
				t.Fatalf("publication changed identity: %v", err)
			}
			yaml := strings.Replace(sampleSpec, "name: recovery", "name: alternate", 1)
			for i := range 2 {
				if _, _, err := repo.PublishPolicyRevision(ctx, scope, p.ID, yaml, fmt.Sprint("revision-", i), false); err != nil {
					t.Fatal(err)
				}
			}
			if _, _, err := repo.InitializeScene(ctx, scope, "another", "Another", "Shared label", sampleSpec, "another"); err != nil {
				t.Fatalf("same key in another Scene: %v", err)
			}
			// Unique keys are enforced even when callers bypass the application.
			if _, err := store.db.ExecContext(ctx, "UPDATE policy SET spec_key='recovery' WHERE policy_id=$1", p.ID); err == nil {
				t.Fatal("database accepted duplicate identity")
			}
			var revisions int
			if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM policy_revision WHERE policy_id=(SELECT id FROM policy WHERE policy_id=$1) AND status='PUBLISHED'", p.ID).Scan(&revisions); err != nil || revisions != 2 {
				t.Fatalf("rejected publication left a Revision: %d %v", revisions, err)
			}
			// Distinct idempotency keys still compete for one business identity.
			results := make(chan error, 8)
			for i := range 8 {
				go func(i int) {
					_, _, err := repo.CreatePolicy(ctx, scope, "recovery", "Concurrent", "concurrent", fmt.Sprint("concurrent-", i))
					results <- err
				}(i)
			}
			created := 0
			for range 8 {
				err := <-results
				if err == nil {
					created++
				} else if !errors.Is(err, spec.ErrSpecKeyConflict) {
					t.Fatalf("concurrent creation: %v", err)
				}
			}
			if created != 1 {
				t.Fatalf("created %d Policies with one key", created)
			}
		})
	}
}

func TestLegacySpecKeys(t *testing.T) {
	for _, backend := range registeredDrivers() {
		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()
			store := identityStore(t, backend.name)
			scope := spec.Scope{WorkspaceID: "default", ProjectID: "default", ActorID: "admin"}
			repo := spec.New(store.Specs())
			initial, _, err := repo.InitializeScene(ctx, scope, "recovery", "Recovery", "Policy", sampleSpec, "init")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.db.ExecContext(ctx, "UPDATE policy SET spec_key=NULL WHERE policy_id=$1", initial.Policy.ID); err != nil {
				t.Fatal(err)
			}
			if p, err := repo.GetPolicy(ctx, scope, initial.Policy.ID); err != nil || p.SpecKey != "" {
				t.Fatalf("legacy read: %+v %v", p, err)
			}
			if _, _, err := repo.CreatePolicy(ctx, scope, "recovery", "New", "recovery", "duplicate"); !errors.Is(err, spec.ErrSpecKeyConflict) {
				t.Fatalf("legacy identity reused: %v", err)
			}
			if _, err := repo.SavePolicyDraft(ctx, scope, initial.Policy.ID, strings.Replace(sampleSpec, "name: recovery", "name: changed", 1)); !errors.Is(err, spec.ErrSpecKeyMismatch) {
				t.Fatalf("legacy identity renamed: %v", err)
			}
			if _, err := repo.SavePolicyDraft(ctx, scope, initial.Policy.ID, ""); err != nil {
				t.Fatal(err)
			}
			if p, err := repo.GetPolicy(ctx, scope, initial.Policy.ID); err != nil || p.SpecKey != "recovery" {
				t.Fatalf("legacy claim: %+v %v", p, err)
			}
			// Reproduce historical duplicates without mutating published YAML.
			if _, err := store.db.ExecContext(ctx, "UPDATE policy SET spec_key=NULL WHERE policy_id=$1", initial.Policy.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := store.db.ExecContext(ctx, `INSERT INTO policy(policy_id,workspace_id,project_id,scene_key,name)
 SELECT 'pol_legacy_duplicate',workspace_id,project_id,scene_key,'Duplicate' FROM policy WHERE policy_id=$1`, initial.Policy.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := store.db.ExecContext(ctx, `INSERT INTO policy_revision(policy_revision_id,policy_id,status,yaml,updated_by,updated_at)
 SELECT 'rev_legacy_duplicate',id,'DRAFT',$1,'admin',1 FROM policy WHERE policy_id='pol_legacy_duplicate'`, sampleSpec); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.SavePolicyDraft(ctx, scope, initial.Policy.ID, sampleSpec); !errors.Is(err, spec.ErrSpecKeyConflict) {
				t.Fatalf("legacy duplicate silently claimed: %v", err)
			}
			if _, err := repo.ActivatePolicyRevision(ctx, scope, initial.Policy.ID, initial.Revision.ID, initial.Policy.ActiveRevisionID); !errors.Is(err, spec.ErrSpecKeyConflict) {
				t.Fatalf("activation ignored legacy duplicate: %v", err)
			}
			if p, err := repo.GetPolicy(ctx, scope, initial.Policy.ID); err != nil || p.SpecKey != "" {
				t.Fatalf("failed claim was persisted: %+v %v", p, err)
			}
		})
	}
}

func TestPreUpgradePolicyCreationRetries(t *testing.T) {
	for _, backend := range registeredDrivers() {
		t.Run(backend.name, func(t *testing.T) {
			store := identityStore(t, backend.name)
			ctx := context.Background()
			scope := spec.Scope{WorkspaceID: "default", ProjectID: "default", ActorID: "admin"}
			repo := spec.New(store.Specs())
			if _, _, err := repo.InitializeScene(ctx, scope, "recovery", "Recovery", "Default", sampleSpec, "init"); err != nil {
				t.Fatal(err)
			}
			raw := strings.Replace(sampleSpec, "name: recovery", "name: legacy", 1)
			// Seed the pre-upgrade shape directly. Managed MySQL correctly forbids
			// UPDATE of an immutable idempotency record after its creation.
			const legacyID = "pol_legacy_retry"
			for _, statement := range []struct {
				query string
				args  []any
			}{
				{"INSERT INTO policy(policy_id,workspace_id,project_id,scene_key,name,maintainers_json) SELECT $1,workspace_id,project_id,scene_key,'Legacy','[\"admin\"]' FROM scene WHERE scene_key='recovery'", []any{legacyID}},
				{"INSERT INTO idempotency_record(workspace_id,project_id,operation_type,idempotency_key,request_digest,resource_type,resource_id,created_at) SELECT workspace_id,project_id,'policy:create','legacy-create',$1,'policy',$2,1 FROM policy WHERE policy_id=$3", []any{modelDigest("recovery\nLegacy"), legacyID, legacyID}},
				{"INSERT INTO policy_revision(policy_revision_id,policy_id,status,yaml,updated_by,updated_at) SELECT 'rev_legacy_retry',id,'DRAFT',$1,'admin',1 FROM policy WHERE policy_id=$2", []any{raw, legacyID}},
			} {
				if _, err := store.db.ExecContext(ctx, statement.query, statement.args...); err != nil {
					t.Fatal(err)
				}
			}
			p, err := repo.GetPolicy(ctx, scope, legacyID)
			if err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"", "legacy"} {
				replay, created, err := repo.CreatePolicy(ctx, scope, "recovery", "Legacy", key, "legacy-create")
				if err != nil || created || replay.ID != p.ID || replay.SpecKey != "" {
					t.Fatalf("legacy retry %q: %+v created=%v err=%v", key, replay, created, err)
				}
			}
			for _, input := range []struct{ scene, name, key string }{
				{"recovery", "Legacy", "renamed"},
				{"recovery", "Changed label", "legacy"},
				{"different", "Legacy", "legacy"},
			} {
				if _, _, err := repo.CreatePolicy(ctx, scope, input.scene, input.name, input.key, "legacy-create"); !errors.Is(err, spec.ErrConflict) {
					t.Fatalf("legacy retry accepted changed input %+v: %v", input, err)
				}
			}
			if _, err := repo.SavePolicyDraft(ctx, scope, p.ID, raw); err != nil {
				t.Fatal(err)
			}
			if replay, created, err := repo.CreatePolicy(ctx, scope, "recovery", "Legacy", "", "legacy-create"); err != nil || created || replay.ID != p.ID || replay.SpecKey != "legacy" {
				t.Fatalf("legacy retry after key registration: %+v %v", replay, err)
			}
			if _, _, err := repo.CreatePolicy(ctx, scope, "recovery", "Missing key", "", "new-create"); !errors.Is(err, spec.ErrInvalid) {
				t.Fatalf("new creation omitted key: %v", err)
			}
			policies, err := repo.ListPolicies(ctx, scope, "recovery")
			if err != nil || len(policies) != 2 {
				t.Fatalf("retries created extra Policies: %+v %v", policies, err)
			}
		})
	}
}
