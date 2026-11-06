package storage

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/alibaba/open-guardrail/internal/application/spec"
	specdomain "github.com/alibaba/open-guardrail/internal/domain/spec"
)

func TestUnsafeSpecValidityCannotInitializeOrPublish(t *testing.T) {
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
			unsafe := strings.Replace(sampleSpec, "validForSeconds: 900", "validForSeconds: 9007199254740993", 1)
			var invalidSpec *specdomain.ValidationError
			if _, created, err := repo.InitializeScene(ctx, scope, "recovery", "Recovery", "default", unsafe, "init"); created || !errors.As(err, &invalidSpec) {
				t.Fatalf("unsafe initialization: created=%v err=%v", created, err)
			}
			for _, table := range []string{"scene", "policy", "policy_revision"} {
				var count int
				if err := store.db.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
					t.Fatalf("unsafe initialization wrote %s: %d %v", table, count, err)
				}
			}
			// A rejected request must not reserve its idempotency key.
			initialized, created, err := repo.InitializeScene(ctx, scope, "recovery", "Recovery", "default", sampleSpec, "init")
			if err != nil || !created {
				t.Fatalf("valid initialization after rejection: %v", err)
			}
			safe := strings.Replace(sampleSpec, "validForSeconds: 900", "validForSeconds: 9007199254740991", 1)
			_, expectedDigest, err := specdomain.Validate(safe)
			if err != nil {
				t.Fatal(err)
			}
			for _, fromDraft := range []bool{false, true} {
				key := "direct"
				if fromDraft {
					key = "draft"
					if _, err := repo.SavePolicyDraft(ctx, scope, initialized.Policy.ID, unsafe); err != nil {
						t.Fatal(err)
					}
				}
				before, err := repo.ListPolicyRevisions(ctx, scope, initialized.Policy.ID, 100, 0)
				if err != nil {
					t.Fatal(err)
				}
				if revision, created, err := repo.PublishPolicyRevision(ctx, scope, initialized.Policy.ID, unsafe, key, fromDraft); created || revision.ID != "" || !errors.As(err, &invalidSpec) {
					t.Fatalf("unsafe publication: revision=%+v created=%v err=%v", revision, created, err)
				}
				after, err := repo.ListPolicyRevisions(ctx, scope, initialized.Policy.ID, 100, 0)
				if err != nil || len(after.Items) != len(before.Items) {
					t.Fatalf("unsafe publication created a Revision: %+v %v", after, err)
				}
				if fromDraft {
					if _, err := repo.SavePolicyDraft(ctx, scope, initialized.Policy.ID, safe); err != nil {
						t.Fatal(err)
					}
				}
				revision, created, err := repo.PublishPolicyRevision(ctx, scope, initialized.Policy.ID, safe, key, fromDraft)
				if err != nil || !created || (revision.Digest == nil || *revision.Digest != expectedDigest) {
					t.Fatalf("safe publication after rejection: revision=%+v created=%v err=%v", revision, created, err)
				}
			}
		})
	}
}
