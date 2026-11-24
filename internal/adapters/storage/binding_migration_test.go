package storage

import (
	"context"
	"testing"
	"testing/fstest"

	runapp "github.com/alibaba/open-guardrail/internal/application/run"
	"github.com/alibaba/open-guardrail/internal/application/spec"
	specdomain "github.com/alibaba/open-guardrail/internal/domain/spec"
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
	scope := spec.Scope{WorkspaceID: "default", ProjectID: "default", ActorID: "admin"}
	// Seed the old schema directly: the current repository requires newer columns.
	for _, query := range []string{
		"INSERT INTO scene(workspace_id,project_id,scene_key,name) SELECT workspace_id,id,'recovery','Recovery' FROM project WHERE project_id='default'",
		"INSERT INTO policy(policy_id,workspace_id,project_id,scene_key,name) SELECT 'pol_legacy',workspace_id,project_id,scene_key,'default' FROM scene WHERE scene_key='recovery'",
	} {
		if _, err := old.db.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	canonical, digest, err := specdomain.Validate(sampleSpec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.db.ExecContext(ctx, `INSERT INTO policy_revision(policy_revision_id,policy_id,status,revision_number,yaml,canonical_json,digest,updated_by,updated_at,published_by,published_at)
 SELECT 'rev_legacy',id,'PUBLISHED',1,$1,$2,$3,'admin',1,'admin',1 FROM policy WHERE policy_id='pol_legacy'`, sampleSpec, canonical, digest); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		"UPDATE policy SET active_revision_id=(SELECT id FROM policy_revision WHERE policy_revision_id='rev_legacy') WHERE policy_id='pol_legacy'",
		"UPDATE scene SET default_policy_id=(SELECT id FROM policy WHERE policy_id='pol_legacy') WHERE scene_key='recovery'",
	} {
		if _, err := old.db.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := old.db.ExecContext(ctx, `INSERT INTO guardrail_run(run_id,workspace_id,project_id,scene_key,policy_id,policy_revision_id,spec_digest,status,change_set_json,change_set_digest,idempotency_key,request_digest,run_ttl_seconds,deadline_at,decision_ttl_seconds,created_by,created_at)
 SELECT 'run_legacy',p.workspace_id,p.project_id,p.scene_key,p.id,r.id,r.digest,'OPEN','{}',$1,'legacy',$2,sc.run_ttl_seconds,1+sc.run_ttl_seconds,900,'admin',1 FROM policy p JOIN scene sc ON sc.default_policy_id=p.id JOIN policy_revision r ON r.id=p.active_revision_id WHERE p.policy_id='pol_legacy'`, modelDigest("{}"), modelDigest("legacy")); err != nil {
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
	revision, err := upgraded.Specs().GetPolicyRevision(ctx, scope, "rev_legacy")
	if err != nil || revision.YAML != sampleSpec {
		t.Fatalf("upgrade changed Spec: %+v %v", revision, err)
	}
	legacy, err := upgraded.Runs().GetPolicyRun(ctx, runapp.Scope{WorkspaceID: "default", ProjectID: "default"}, "run_legacy")
	if err != nil || legacy.Selection != nil || legacy.PolicyRevisionID != "rev_legacy" {
		t.Fatalf("legacy evidence was reconstructed: %+v %v", legacy, err)
	}
	page, err := upgraded.Specs().ListBindings(ctx, scope, "recovery", 20, nil)
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("new binding table: %+v %v", page, err)
	}
	if p, err := upgraded.Specs().GetPolicy(ctx, scope, "pol_legacy"); err != nil || p.SpecKey != "" {
		t.Fatalf("upgrade rewrote legacy identity: %+v %v", p, err)
	}
	if _, err := upgraded.Specs().SavePolicyDraft(ctx, scope, "pol_legacy", sampleSpec); err != nil {
		t.Fatal(err)
	}
	if p, err := upgraded.Specs().GetPolicy(ctx, scope, "pol_legacy"); err != nil || p.SpecKey != "recovery" {
		t.Fatalf("first write did not register legacy identity: %+v %v", p, err)
	}
}
