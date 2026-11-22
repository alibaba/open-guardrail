package storage

import (
	"context"
	"errors"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/alibaba/open-guardrail/internal/application/spec"
	specdomain "github.com/alibaba/open-guardrail/internal/domain/spec"
	"github.com/alibaba/open-guardrail/migrations"
)

func initializeLegacyPolicy(ctx context.Context, old *Store, scope spec.Scope) (spec.InitializedScene, bool, error) {
	canonical, digest, err := specdomain.Validate(sampleSpec)
	if err != nil {
		return spec.InitializedScene{}, false, err
	}
	tx, err := old.db.BeginTx(ctx, nil)
	if err != nil {
		return spec.InitializedScene{}, false, err
	}
	defer tx.Rollback()
	statements := []struct {
		query string
		args  []any
	}{
		{"INSERT INTO scene(workspace_id,project_id,scene_key,name) VALUES((SELECT id FROM workspace WHERE workspace_id=$1),(SELECT id FROM project WHERE project_id=$2),'recovery','Recovery')", []any{scope.WorkspaceID, scope.ProjectID}},
		{"INSERT INTO policy(policy_id,workspace_id,project_id,scene_key,name) VALUES('pol_legacy',(SELECT id FROM workspace WHERE workspace_id=$1),(SELECT id FROM project WHERE project_id=$2),'recovery','Default')", []any{scope.WorkspaceID, scope.ProjectID}},
		{"INSERT INTO policy_revision(policy_revision_id,policy_id,status,revision_number,yaml,canonical_json,digest,published_at,published_by,updated_at,updated_by) VALUES('rev_legacy',(SELECT id FROM policy WHERE policy_id='pol_legacy'),'PUBLISHED',1,$1,$2,$3,1,$4,1,$4)", []any{sampleSpec, canonical, digest, scope.ActorID}},
		{"UPDATE policy SET active_revision_id=(SELECT id FROM policy_revision WHERE policy_revision_id='rev_legacy') WHERE policy_id='pol_legacy'", nil},
		{"UPDATE scene SET default_policy_id=(SELECT id FROM policy WHERE policy_id='pol_legacy') WHERE scene_key='recovery'", nil},
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement.query, statement.args...); err != nil {
			return spec.InitializedScene{}, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return spec.InitializedScene{}, false, err
	}
	return spec.InitializedScene{Policy: spec.Policy{ID: "pol_legacy"}, Revision: spec.PolicyRevision{ID: "rev_legacy"}}, true, nil
}

func TestMaintainerMigrationUsesOnlyRecordedCreators(t *testing.T) {
	ctx := context.Background()
	c := testConfig(t, "sqlite")
	files := fstest.MapFS{}
	for _, name := range []string{"sqlite/00001_initial.sql", "sqlite/00002_policy_binding.sql", "sqlite/00003_binding_audit.sql"} {
		data, err := migrations.Files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = &fstest.MapFile{Data: data}
	}
	old, err := openWithMigrations(ctx, c, files)
	if err != nil {
		t.Fatal(err)
	}
	scope := spec.Scope{WorkspaceID: "default", ProjectID: "default", ActorID: "admin"}
	if _, _, err := initializeLegacyPolicy(ctx, old, scope); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"pol_audit", "pol_unknown"} {
		if _, err := old.db.ExecContext(ctx, "INSERT INTO policy(policy_id,workspace_id,project_id,scene_key,name) SELECT $1,workspace_id,project_id,scene_key,$1 FROM policy WHERE policy_id='pol_legacy'", id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := old.db.ExecContext(ctx, "INSERT INTO audit_event(event_type,actor_id,workspace_id,project_id,occurred_at,resource_type,resource_id) SELECT 'policy_created','creator',workspace_id,project_id,1,'policy',policy_id FROM policy WHERE policy_id='pol_audit'"); err != nil {
		t.Fatal(err)
	}
	old.Close()
	upgraded, err := Open(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	for id, owners := range map[string][]string{"pol_legacy": {"admin"}, "pol_audit": {"creator"}, "pol_unknown": {}} {
		p, err := upgraded.Specs().GetPolicy(ctx, scope, id)
		if err != nil || !slices.Equal(p.Maintainers, owners) {
			t.Fatalf("backfill %s: %+v %v", id, p, err)
		}
	}
	if _, err := upgraded.Specs().SavePolicyDraft(ctx, scope, "pol_unknown", sampleSpec); !errors.Is(err, spec.ErrForbidden) {
		t.Fatalf("unknown legacy owner became writable: %v", err)
	}
	if _, err := upgraded.Specs().SetPolicyMaintainers(ctx, scope, "pol_unknown", []string{"admin"}); !errors.Is(err, spec.ErrForbidden) {
		t.Fatalf("administrator claimed ownerless Spec: %v", err)
	}
	if _, err := upgraded.Specs().SavePolicyDraft(ctx, scope, "pol_legacy", sampleSpec); err != nil {
		t.Fatal(err)
	}
}
