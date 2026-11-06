package storage

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/alibaba/open-guardrail/internal/application/spec"
)

func TestPolicyLifecycleTransactions(t *testing.T) {
	for _, backend := range registeredDrivers() {
		t.Run(backend.name, func(t *testing.T) { testPolicyLifecycleTransactionsBackend(t, identityStore(t, backend.name)) })
	}
}

func TestDraftPublicationChecksReviewedContentAndRetryIdentity(t *testing.T) {
	for _, backend := range registeredDrivers() {
		t.Run(backend.name, func(t *testing.T) {
			store := identityStore(t, backend.name)
			ctx := context.Background()
			for _, query := range []string{"INSERT INTO workspace(workspace_id,name) VALUES('w','w')", "INSERT INTO project(project_id,workspace_id,name) VALUES('p',(SELECT id FROM workspace WHERE workspace_id='w'),'p')"} {
				if _, err := store.db.ExecContext(ctx, query); err != nil {
					t.Fatal(err)
				}
			}
			scope := spec.Scope{WorkspaceID: "w", ProjectID: "p", ActorID: "admin"}
			repo := spec.New(store.Specs())
			initialized, _, err := repo.InitializeScene(ctx, scope, "recovery", "Recovery", "default", sampleSpec, "init")
			if err != nil {
				t.Fatal(err)
			}
			id := initialized.Policy.ID
			replacement := strings.Replace(sampleSpec, "Inspect current state", "Inspect replacement state", 1)
			if _, err := repo.SavePolicyDraft(ctx, scope, id, replacement); err != nil {
				t.Fatal(err)
			}
			if _, _, err := repo.PublishPolicyRevision(ctx, scope, id, sampleSpec, "stale", true); !errors.Is(err, spec.ErrConflict) {
				t.Fatalf("published another session's Draft: %v", err)
			}
			page, err := repo.ListPolicyRevisions(ctx, scope, id, 100, 0)
			if err != nil || len(page.Items) != 1 {
				t.Fatalf("stale publication created a Revision: %+v %v", page, err)
			}
			published, created, err := repo.PublishPolicyRevision(ctx, scope, id, replacement, "retry", true)
			if err != nil || !created || published.YAML != replacement {
				t.Fatalf("publication=%+v created=%v err=%v", published, created, err)
			}
			if _, err := repo.SavePolicyDraft(ctx, scope, id, sampleSpec); err != nil {
				t.Fatal(err)
			}
			replayed, created, err := repo.PublishPolicyRevision(ctx, scope, id, replacement, "retry", true)
			if err != nil || created || replayed.ID != published.ID {
				t.Fatalf("exact retry changed after Draft edit: %+v %v", replayed, err)
			}
			if _, _, err := repo.PublishPolicyRevision(ctx, scope, id, sampleSpec, "retry", true); !errors.Is(err, spec.ErrConflict) {
				t.Fatalf("same key accepted different reviewed content: %v", err)
			}
		})
	}
}
func testPolicyLifecycleTransactionsBackend(t *testing.T, store *Store) {
	ctx := context.Background()
	scope := spec.Scope{WorkspaceID: "w", ProjectID: "p", ActorID: "admin"}
	if _, err := store.db.ExecContext(ctx, "INSERT INTO workspace(workspace_id,name) VALUES('w','w')"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, "INSERT INTO project(project_id,workspace_id,name) VALUES('p',(SELECT id FROM workspace WHERE workspace_id='w'),'p')"); err != nil {
		t.Fatal(err)
	}
	repo := spec.New(store.Specs())
	initialized, created, err := repo.InitializeScene(ctx, scope, "recovery", "Recovery", "default", sampleSpec, "init-1")
	if err != nil || !created || initialized.Policy.ActiveRevisionID == nil || initialized.Scene.DefaultPolicyID == nil {
		t.Fatalf("initialized=%+v created=%v err=%v", initialized, created, err)
	}
	repeated, created, err := repo.InitializeScene(ctx, scope, "recovery", "Recovery", "default", sampleSpec, "init-1")
	if err != nil || created || repeated.Policy.ID != initialized.Policy.ID {
		t.Fatalf("repeat=%+v created=%v err=%v", repeated, created, err)
	}
	if _, _, err := repo.InitializeScene(ctx, scope, "recovery", "Recovery", "default", sampleSpec, "init-2"); !errors.Is(err, spec.ErrSceneAlreadyInitialized) {
		t.Fatalf("second init=%v", err)
	}
	if _, err := repo.SetPolicyArchived(ctx, scope, initialized.Policy.ID, true); !errors.Is(err, spec.ErrConflict) {
		t.Fatalf("archive default=%v", err)
	}
	alternate, created, err := repo.CreatePolicy(ctx, scope, "recovery", "alternate", "policy-1")
	if err != nil || !created {
		t.Fatalf("policy=%+v created=%v err=%v", alternate, created, err)
	}
	alternateAgain, created, err := repo.CreatePolicy(ctx, scope, "recovery", "alternate", "policy-1")
	if err != nil || created || alternateAgain.ID != alternate.ID {
		t.Fatalf("replay=%+v created=%v err=%v", alternateAgain, created, err)
	}
	draft, err := repo.SavePolicyDraft(ctx, scope, alternate.ID, sampleSpec)
	if err != nil || draft.YAML != sampleSpec {
		t.Fatalf("draft=%+v err=%v", draft, err)
	}
	first, created, err := repo.PublishPolicyRevision(ctx, scope, alternate.ID, sampleSpec, "publish-draft", true)
	if err != nil || !created || (first.RevisionNumber == nil || *first.RevisionNumber != 1) {
		t.Fatalf("first=%+v created=%v err=%v", first, created, err)
	}
	if _, err := repo.SavePolicyDraft(ctx, scope, alternate.ID, "invalid: ["); err != nil {
		t.Fatal(err)
	}
	firstAgain, created, err := repo.PublishPolicyRevision(ctx, scope, alternate.ID, sampleSpec, "publish-draft", true)
	if err != nil || created || firstAgain.ID != first.ID {
		t.Fatalf("draft replay=%+v created=%v err=%v", firstAgain, created, err)
	}
	direct, created, err := repo.PublishPolicyRevision(ctx, scope, alternate.ID, sampleSpec, "publish-direct", false)
	if err != nil || !created || (direct.RevisionNumber == nil || *direct.RevisionNumber != 2) {
		t.Fatalf("direct=%+v created=%v err=%v", direct, created, err)
	}
	unchanged, err := repo.GetPolicyDraft(ctx, scope, alternate.ID)
	if err != nil || unchanged.YAML != "invalid: [" {
		t.Fatalf("direct publish changed draft: %+v %v", unchanged, err)
	}
	activated, err := repo.ActivatePolicyRevision(ctx, scope, alternate.ID, direct.ID, nil)
	if err != nil || activated.ActiveRevisionID == nil || *activated.ActiveRevisionID != direct.ID {
		t.Fatalf("activate=%+v err=%v", activated, err)
	}
	scene, err := repo.SetDefaultPolicy(ctx, scope, "recovery", alternate.ID, &initialized.Policy.ID)
	if err != nil || scene.DefaultPolicyID == nil || *scene.DefaultPolicyID != alternate.ID {
		t.Fatalf("default=%+v err=%v", scene, err)
	}
	suspended, err := repo.SavePolicyDraft(ctx, scope, initialized.Policy.ID, sampleSpec)
	if err != nil {
		t.Fatal(err)
	}
	archived, err := repo.SetPolicyArchived(ctx, scope, initialized.Policy.ID, true)
	if err != nil || archived.LifecycleState != "ARCHIVED" {
		t.Fatalf("archived=%+v err=%v", archived, err)
	}
	if _, err := repo.SavePolicyDraft(ctx, scope, archived.ID, sampleSpec); !errors.Is(err, spec.ErrConflict) {
		t.Fatalf("archived draft=%v", err)
	}
	readOnly, err := repo.GetPolicyDraft(ctx, scope, archived.ID)
	if err != nil || readOnly.ID != suspended.ID || readOnly.YAML != sampleSpec || readOnly.Status != "DRAFT" {
		t.Fatalf("archiving changed or hid draft: %+v %v", readOnly, err)
	}
	if _, _, err := repo.PublishPolicyRevision(ctx, scope, archived.ID, sampleSpec, "archived-draft", true); !errors.Is(err, spec.ErrConflict) {
		t.Fatalf("archived draft published: %v", err)
	}
	restored, err := repo.SetPolicyArchived(ctx, scope, archived.ID, false)
	if err != nil || restored.LifecycleState != "ACTIVE" {
		t.Fatalf("restored=%+v err=%v", restored, err)
	}
	replacement := strings.Replace(sampleSpec, "Inspect current state", "Inspect restored state", 1)
	resumed, err := repo.SavePolicyDraft(ctx, scope, restored.ID, replacement)
	if err != nil || resumed.ID != suspended.ID || resumed.YAML != replacement || resumed.Status != "DRAFT" {
		t.Fatalf("restoring did not resume the same draft: %+v %v", resumed, err)
	}
	var draftAudits int
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_event WHERE event_type='policy_draft_saved' AND resource_type='revision' AND resource_id=$1", suspended.ID).Scan(&draftAudits); err != nil || draftAudits != 2 {
		t.Fatalf("draft audit must reference the edited Revision: count=%d err=%v", draftAudits, err)
	}
	ttl, err := repo.SetSceneTTL(ctx, scope, "recovery", 604800)
	if err != nil || ttl.RunTTLSeconds != 604800 {
		t.Fatalf("ttl=%+v err=%v", ttl, err)
	}
	for _, event := range []struct{ name, kind, id string }{
		{"scene_initialized", "scene", "recovery"},
		{"policy_created", "policy", alternate.ID},
		{"policy_revision_published", "revision", first.ID},
		{"policy_revision_activated", "policy", alternate.ID},
		{"scene_default_policy_changed", "scene", "recovery"},
	} {
		var count int
		if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_event WHERE event_type=$1 AND resource_type=$2 AND resource_id=$3", event.name, event.kind, event.id).Scan(&count); err != nil || count != 1 {
			t.Fatalf("audit %s %s/%s count=%d err=%v", event.name, event.kind, event.id, count, err)
		}
	}
}
