package storage

import (
	"context"
	"database/sql"
	"errors"
	runapp "github.com/alibaba/open-guardrail/internal/application/run"
	"github.com/alibaba/open-guardrail/internal/application/spec"
	"testing"
	"time"
)

func TestSingleDraftKeepsIdentityUntilPublication(t *testing.T) {
	for _, backend := range registeredDrivers() {
		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()
			store := identityStore(t, backend.name)
			for _, query := range []string{"INSERT INTO workspace(workspace_id,name) VALUES('w','Workspace')", "INSERT INTO project(project_id,workspace_id,name) VALUES('p',(SELECT id FROM workspace WHERE workspace_id='w'),'Project')"} {
				if _, err := store.db.ExecContext(ctx, query); err != nil {
					t.Fatal(err)
				}
			}
			scope := spec.Scope{WorkspaceID: "w", ProjectID: "p", ActorID: "admin"}
			service := spec.New(store.Specs())
			initialized, _, err := service.InitializeScene(ctx, scope, "recovery", "Recovery", "Policy", sampleSpec, "init")
			if err != nil {
				t.Fatal(err)
			}
			var firstID string
			for i := 0; i < 10; i++ {
				draft, err := service.SavePolicyDraft(ctx, scope, initialized.Policy.ID, sampleSpec)
				if err != nil || draft.ID == "" {
					t.Fatalf("save draft: %+v %v", draft, err)
				}
				if i == 0 {
					firstID = draft.ID
				}
				if draft.ID != firstID {
					t.Fatal("editing allocated a new draft")
				}
			}
			readDraft, err := service.GetPolicyRevision(ctx, scope, firstID)
			if err != nil || readDraft.Status != "DRAFT" || readDraft.RevisionNumber != nil || readDraft.Digest != nil || readDraft.PublishedAt != nil || readDraft.PublishedBy != nil {
				t.Fatalf("draft Revision lookup: %+v %v", readDraft, err)
			}
			var count int
			var number, publishedAt sql.NullInt64
			var canonical, digest, publishedBy sql.NullString
			if err := store.db.QueryRowContext(ctx, "SELECT revision_number,canonical_json,digest,published_by,published_at FROM policy_revision WHERE policy_revision_id=$1", firstID).Scan(&number, &canonical, &digest, &publishedBy, &publishedAt); err != nil {
				t.Fatal(err)
			}
			if number.Valid || canonical.Valid || digest.Valid || publishedBy.Valid || publishedAt.Valid {
				t.Fatal("draft has publication-only metadata")
			}
			if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM policy_revision WHERE policy_id=(SELECT id FROM policy WHERE policy_id=$1) AND status='DRAFT'", initialized.Policy.ID).Scan(&count); err != nil || count != 1 {
				t.Fatalf("draft count: %d %v", count, err)
			}
			if _, err := store.db.ExecContext(ctx, "INSERT INTO policy_revision(policy_revision_id,policy_id,status,yaml,updated_by,updated_at) SELECT 'rev_duplicate',id,'DRAFT','','admin',1 FROM policy WHERE policy_id=$1", initialized.Policy.ID); err == nil {
				t.Fatal("second draft accepted")
			}
			if _, _, err := service.PublishPolicyRevision(ctx, scope, initialized.Policy.ID, sampleSpec, "publish", true); err != nil {
				t.Fatal(err)
			}
			saved, err := service.GetPolicyDraft(ctx, scope, initialized.Policy.ID)
			if !errors.Is(err, spec.ErrNotFound) {
				t.Fatalf("published draft remains editable: %+v %v", saved, err)
			}
			published, err := service.GetPolicyRevision(ctx, scope, firstID)
			if err != nil || published.Status != "PUBLISHED" || (published.RevisionNumber == nil || *published.RevisionNumber != 2) || (published.PublishedAt == nil || published.UpdatedAt != *published.PublishedAt) || (published.PublishedBy == nil || published.UpdatedBy != *published.PublishedBy) {
				t.Fatalf("publication: %+v %v", published, err)
			}
			next, err := service.SavePolicyDraft(ctx, scope, initialized.Policy.ID, "incomplete: [")
			if err != nil || next.ID == "" || next.ID == firstID {
				t.Fatalf("new draft: %+v %v", next, err)
			}
			if _, err := store.db.ExecContext(ctx, "UPDATE policy_revision SET yaml='changed' WHERE policy_revision_id=$1", firstID); err == nil {
				t.Fatal("published revision changed")
			}
			if _, err := store.db.ExecContext(ctx, "UPDATE policy SET active_revision_id=(SELECT id FROM policy_revision WHERE policy_revision_id=$1) WHERE policy_id=$2", next.ID, initialized.Policy.ID); err == nil {
				t.Fatal("draft became active")
			}
			runScope := runapp.Scope{WorkspaceID: "w", ProjectID: "p", ActorID: "test", CanSelectRevision: true}
			_, created, err := store.Runs().CreatePolicyRun(ctx, runScope, runapp.PolicyRunCreateInput{SceneKey: "recovery", PolicyRevisionID: next.ID, CanSelectRevision: true, IdempotencyKey: "draft-run", Now: time.Now().Unix()})
			if !errors.Is(err, runapp.ErrSpecNotPublished) || created {
				t.Fatalf("Run draft selection: created=%v err=%v", created, err)
			}
		})
	}
}

func TestDraftPublicationDoesNotCommitWhenUpdateIsSkipped(t *testing.T) {
	ctx := context.Background()
	store := identityStore(t, "sqlite")
	for _, query := range []string{
		"INSERT INTO workspace(workspace_id,name) VALUES('w','Workspace')",
		"INSERT INTO project(project_id,workspace_id,name) VALUES('p',(SELECT id FROM workspace WHERE workspace_id='w'),'Project')",
	} {
		if _, err := store.db.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	scope := spec.Scope{WorkspaceID: "w", ProjectID: "p", ActorID: "admin"}
	service := spec.New(store.Specs())
	initialized, _, err := service.InitializeScene(ctx, scope, "recovery", "Recovery", "Policy", sampleSpec, "init")
	if err != nil {
		t.Fatal(err)
	}
	draft, err := service.SavePolicyDraft(ctx, scope, initialized.Policy.ID, sampleSpec)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate an accepted SQL statement that does not update its target row.
	if _, err := store.db.ExecContext(ctx, `CREATE TRIGGER skip_draft_publication BEFORE UPDATE ON policy_revision
 WHEN NEW.status='PUBLISHED' BEGIN SELECT RAISE(IGNORE); END`); err != nil {
		t.Fatal(err)
	}
	if _, created, err := service.PublishPolicyRevision(ctx, scope, initialized.Policy.ID, sampleSpec, "retry-after-no-update", true); !errors.Is(err, spec.ErrConflict) || created {
		t.Fatalf("publication without a write: created=%v err=%v", created, err)
	}
	unchanged, err := service.GetPolicyDraft(ctx, scope, initialized.Policy.ID)
	if err != nil || unchanged.ID != draft.ID || unchanged.Status != "DRAFT" || unchanged.RevisionNumber != nil || unchanged.PublishedAt != nil {
		t.Fatalf("failed publication changed draft: %+v %v", unchanged, err)
	}
	var auditCount int
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_event WHERE event_type='policy_revision_published' AND resource_id=$1", draft.ID).Scan(&auditCount); err != nil || auditCount != 0 {
		t.Fatalf("failed publication emitted success audit: count=%d err=%v", auditCount, err)
	}
	if _, err := store.db.ExecContext(ctx, "DROP TRIGGER skip_draft_publication"); err != nil {
		t.Fatal(err)
	}
	published, created, err := service.PublishPolicyRevision(ctx, scope, initialized.Policy.ID, sampleSpec, "retry-after-no-update", true)
	if err != nil || !created || published.ID != draft.ID || published.Status != "PUBLISHED" || published.RevisionNumber == nil || *published.RevisionNumber != 2 {
		t.Fatalf("failed publication polluted retry identity: %+v created=%v err=%v", published, created, err)
	}
}
