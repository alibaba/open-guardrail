package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	runapp "github.com/alibaba/open-guardrail/internal/application/run"
	"github.com/alibaba/open-guardrail/internal/application/spec"
	"github.com/alibaba/open-guardrail/internal/domain/resource"
)

func TestBindingSelectionAndRunIsolation(t *testing.T) {
	for _, backend := range registeredDrivers() {
		t.Run(backend.name, func(t *testing.T) {
			store := identityStore(t, backend.name)
			ctx := context.Background()
			seedBindingScope(t, store)
			scope := spec.Scope{WorkspaceID: "default", ProjectID: "default", ActorID: "admin"}
			specs := spec.New(store.Specs())
			initial, _, err := specs.InitializeScene(ctx, scope, "recovery", "Recovery", "default", sampleSpec, "init")
			if err != nil {
				t.Fatal(err)
			}
			policy, _, err := specs.CreatePolicy(ctx, scope, "recovery", "bound", "new-policy")
			if err != nil {
				t.Fatal(err)
			}
			first, _, err := specs.PublishPolicyRevision(ctx, scope, policy.ID, sampleSpec, "rev-1", false)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := specs.ActivatePolicyRevision(ctx, scope, policy.ID, first.ID, nil); err != nil {
				t.Fatal(err)
			}
			ref := resource.Reference{Kind: "application", ID: "7"}
			put := func(b spec.Binding) {
				t.Helper()
				if _, err := specs.PutBinding(ctx, scope, "recovery", b); err != nil {
					t.Fatal(err)
				}
			}
			binding := spec.Binding{Resource: ref, PolicyID: policy.ID, PolicyRevisionID: &first.ID}
			put(binding)
			put(binding)
			page, err := specs.ListBindings(ctx, scope, "recovery", 100, nil)
			if err != nil || len(page.Items) != 1 {
				t.Fatalf("duplicate PUT: %+v %v", page, err)
			}
			second, _, err := specs.PublishPolicyRevision(ctx, scope, policy.ID, sampleSpec, "rev-2", false)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := specs.ActivatePolicyRevision(ctx, scope, policy.ID, second.ID, &first.ID); err != nil {
				t.Fatal(err)
			}
			runs := runapp.New(store.Runs())
			runScope := runapp.Scope{WorkspaceID: scope.WorkspaceID, ProjectID: scope.ProjectID, ActorID: "agent"}
			create := func(key string, refs ...resource.Reference) (runapp.PolicyRun, error) {
				t.Helper()
				run, _, _, err := runs.Create(ctx, runScope, runapp.CreateInput{SceneKey: "recovery", ChangeSet: json.RawMessage(`{}`), Resources: refs, IdempotencyKey: key})
				return run, err
			}
			pinned, err := create("pinned", ref)
			if err != nil || pinned.PolicyRevisionID != first.ID || pinned.Selection == nil || pinned.Selection.Source != "binding" || pinned.Selection.RevisionMode != "pinned" || len(pinned.Selection.Resources) != 1 || pinned.Selection.Resources[0] != ref {
				t.Fatalf("pinned selection: %+v %v", pinned, err)
			}
			binding.PolicyRevisionID = nil
			put(binding)
			follow, err := create("follow", ref)
			if err != nil || follow.PolicyRevisionID != second.ID || follow.Selection == nil || follow.Selection.RevisionMode != "active" {
				t.Fatalf("active selection: %+v %v", follow, err)
			}
			replay, err := create("pinned", ref)
			if err != nil || replay.ID != pinned.ID || replay.PolicyRevisionID != first.ID || replay.Selection == nil || replay.Selection.RevisionMode != "pinned" {
				t.Fatalf("retry changed frozen Run: %+v %v", replay, err)
			}
			overrideScope := runScope
			overrideScope.CanSelectRevision = true
			override, _, _, err := runs.Create(ctx, overrideScope, runapp.CreateInput{SceneKey: "recovery", ChangeSet: json.RawMessage(`{}`), RevisionID: &first.ID, IdempotencyKey: "explicit"})
			if err != nil || override.PolicyRevisionID != first.ID || override.Selection != nil {
				t.Fatalf("explicit evidence: %+v %v", override, err)
			}
			if _, err := store.db.ExecContext(ctx, "UPDATE guardrail_run SET selection_json='{}' WHERE run_id=$1", pinned.ID); err == nil {
				t.Fatal("selection evidence is mutable")
			}
			other := resource.Reference{Kind: "application", ID: "8"}
			if _, err := create("partial", ref, other); !errors.Is(err, runapp.ErrBindingConflict) {
				t.Fatalf("partial bindings: %v", err)
			}
			put(spec.Binding{Resource: other, PolicyID: initial.Policy.ID})
			if _, err := create("conflict", ref, other); !errors.Is(err, runapp.ErrBindingConflict) {
				t.Fatalf("conflicting bindings: %v", err)
			}
			page, err = specs.ListBindings(ctx, scope, "recovery", 1, nil)
			if err != nil || page.NextAfter == nil {
				t.Fatalf("first page: %+v %v", page, err)
			}
			next, err := specs.ListBindings(ctx, scope, "recovery", 1, page.NextAfter)
			if err != nil || len(next.Items) != 1 || next.NextAfter != nil || next.Items[0].Resource != other {
				t.Fatalf("next page: %+v %v", next, err)
			}
			// Cross-project, another Policy's Revision and Drafts
			// cannot be made valid by going through a binding.
			wrongScope := scope
			wrongScope.ProjectID = "other"
			if _, err := specs.PutBinding(ctx, wrongScope, "recovery", binding); !errors.Is(err, spec.ErrNotFound) {
				t.Fatalf("cross scope: %v", err)
			}
			if _, err := specs.PutBinding(ctx, scope, "recovery", spec.Binding{Resource: ref, PolicyID: policy.ID, PolicyRevisionID: &initial.Revision.ID}); !errors.Is(err, spec.ErrInvalid) {
				t.Fatalf("another Policy revision: %v", err)
			}
			draft, err := specs.SavePolicyDraft(ctx, scope, policy.ID, sampleSpec)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := specs.PutBinding(ctx, scope, "recovery", spec.Binding{Resource: ref, PolicyID: policy.ID, PolicyRevisionID: &draft.ID}); !errors.Is(err, spec.ErrInvalid) {
				t.Fatalf("draft binding: %v", err)
			}
			if _, err := store.db.ExecContext(ctx, `INSERT INTO policy_binding(workspace_id,project_id,scene_key,resource_kind,resource_id,policy_id,policy_revision_id)
 VALUES((SELECT id FROM workspace WHERE workspace_id=$1),(SELECT id FROM project WHERE project_id=$2),'recovery','application','invalid-fk',(SELECT id FROM policy WHERE policy_id=$3),(SELECT id FROM policy_revision WHERE policy_revision_id=$4))`, scope.WorkspaceID, scope.ProjectID, policy.ID, initial.Revision.ID); err == nil {
				t.Fatal("database accepted another Policy's Revision in a binding")
			}
			if _, err := specs.SetPolicyArchived(ctx, scope, policy.ID, true); err != nil {
				t.Fatal(err)
			}
			if _, err := create("archived", ref); !errors.Is(err, runapp.ErrSpecNotPublished) {
				t.Fatalf("archived binding fell back: %v", err)
			}
			for range 2 {
				if err := specs.DeleteBinding(ctx, scope, "recovery", ref); err != nil {
					t.Fatal(err)
				}
			}
			fallback, err := create("unbound", ref)
			if err != nil || fallback.PolicyID != initial.Policy.ID || fallback.Selection != nil {
				t.Fatalf("unbound fallback: %+v %v", fallback, err)
			}
			put(spec.Binding{Resource: ref, PolicyID: initial.Policy.ID})
			replay, err = create("unbound", ref)
			if err != nil || replay.ID != fallback.ID || replay.PolicyRevisionID != fallback.PolicyRevisionID || replay.Selection != nil {
				t.Fatalf("retry added selection evidence: %+v %v", replay, err)
			}
			plain, err := create("no-resources")
			if err != nil || plain.PolicyID != initial.Policy.ID || plain.Selection != nil {
				t.Fatalf("default selection: %+v %v", plain, err)
			}
			for _, run := range []runapp.PolicyRun{override, fallback, plain} {
				var stored sql.NullString
				if err := store.db.QueryRowContext(ctx, "SELECT selection_json FROM guardrail_run WHERE run_id=$1", run.ID).Scan(&stored); err != nil || stored.Valid {
					t.Fatalf("expected SQL NULL selection for %s: %+v %v", run.ID, stored, err)
				}
				read, err := store.Runs().GetPolicyRun(ctx, runScope, run.ID)
				if err != nil || read.Selection != nil || read.PolicyRevisionID != run.PolicyRevisionID {
					t.Fatalf("optional selection readback: %+v %v", read, err)
				}
			}
		})
	}
}

// Keep fixture initialization independent of optional authentication modes.
func seedBindingScope(t *testing.T, store *Store) {
	t.Helper()
	for _, query := range []string{
		"INSERT INTO workspace(workspace_id,name) VALUES('default','Default')",
		"INSERT INTO project(project_id,workspace_id,name) VALUES('default',(SELECT id FROM workspace WHERE workspace_id='default'),'Default')",
	} {
		if _, err := store.db.ExecContext(context.Background(), query); err != nil {
			t.Fatal(err)
		}
	}
}
