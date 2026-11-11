package storage

import (
	"context"
	"testing"
	"testing/fstest"

	runapp "github.com/alibaba/open-guardrail/internal/application/run"
	"github.com/alibaba/open-guardrail/internal/application/spec"
	"github.com/alibaba/open-guardrail/migrations"
)

func TestBindingMigrationPreservesExistingPolicyAndRevision(t *testing.T) {
	ctx := context.Background()
	c := testConfig(t, "sqlite")
	schema, err := migrations.Files.ReadFile("sqlite/00001_initial.sql")
	if err != nil {
		t.Fatal(err)
	}
	old, err := openWithMigrations(ctx, c, fstest.MapFS{"sqlite/00001_initial.sql": {Data: schema}})
	if err != nil {
		t.Fatal(err)
	}
	seedBindingScope(t, old)
	scope := spec.Scope{WorkspaceID: "default", ProjectID: "default", ActorID: "admin"}
	initial, _, err := spec.New(old.Specs()).InitializeScene(ctx, scope, "recovery", "Recovery", "default", sampleSpec, "init")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.db.ExecContext(ctx, `INSERT INTO guardrail_run(run_id,workspace_id,project_id,scene_key,policy_id,policy_revision_id,spec_digest,status,change_set_json,change_set_digest,idempotency_key,request_digest,run_ttl_seconds,deadline_at,decision_ttl_seconds,created_by,created_at)
 SELECT 'run_legacy',p.workspace_id,p.project_id,p.scene_key,p.id,r.id,r.digest,'OPEN','{}',$1,'legacy',$2,sc.run_ttl_seconds,1+sc.run_ttl_seconds,900,'admin',1 FROM policy p JOIN scene sc ON sc.default_policy_id=p.id JOIN policy_revision r ON r.id=p.active_revision_id WHERE p.policy_id=$3`, modelDigest("{}"), modelDigest("legacy"), initial.Policy.ID); err != nil {
		t.Fatal(err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	upgraded, err := Open(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	revision, err := upgraded.Specs().GetPolicyRevision(ctx, scope, initial.Revision.ID)
	if err != nil || revision.YAML != sampleSpec {
		t.Fatalf("upgrade changed Spec: %+v %v", revision, err)
	}
	legacy, err := upgraded.Runs().GetPolicyRun(ctx, runapp.Scope{WorkspaceID: "default", ProjectID: "default"}, "run_legacy")
	if err != nil || legacy.Selection != nil || legacy.PolicyRevisionID != initial.Revision.ID {
		t.Fatalf("legacy evidence was reconstructed: %+v %v", legacy, err)
	}
	page, err := upgraded.Specs().ListBindings(ctx, scope, "recovery", 20, nil)
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("new binding table: %+v %v", page, err)
	}
}
