package storage

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/alibaba/open-guardrail/internal/application/spec"
	"github.com/alibaba/open-guardrail/migrations"
)

func TestSQLiteDraftRunMigrationRollsBackAndPreservesReferences(t *testing.T) {
	ctx := context.Background()
	config := testConfig(t, "sqlite")
	previous, broken := fstest.MapFS{}, fstest.MapFS{}
	entries, err := migrations.Files.ReadDir("sqlite")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		data, err := migrations.Files.ReadFile("sqlite/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if name < "00011" {
			previous["sqlite/"+name] = &fstest.MapFile{Data: data}
		}
		if name == "00011_draft_run_snapshot.sql" {
			data = []byte(strings.Replace(string(data), "DROP TABLE guardrail_run;", "DROP TABLE guardrail_run;\nTHIS IS INVALID SQL;", 1))
		}
		broken["sqlite/"+name] = &fstest.MapFile{Data: data}
	}
	old, err := openWithMigrations(ctx, config, previous)
	if err != nil {
		t.Fatal(err)
	}
	initialized, _, err := spec.New(old.Specs()).InitializeScene(ctx, spec.Scope{WorkspaceID: "default", ProjectID: "default", ActorID: "admin"}, "recovery", "Recovery", "Policy", sampleSpec, "init")
	if err != nil {
		t.Fatal(err)
	}
	_, err = old.db.ExecContext(ctx, `INSERT INTO guardrail_run(run_id,workspace_id,project_id,scene_key,policy_id,policy_revision_id,spec_digest,status,change_set_json,change_set_digest,idempotency_key,request_digest,run_ttl_seconds,deadline_at,decision_ttl_seconds,created_by,created_at)
	 SELECT 'run_before_upgrade',p.workspace_id,p.project_id,p.scene_key,p.id,r.id,r.digest,'OPEN','{}',$1,'old',$2,3600,4600,600,'admin',1000
	 FROM policy p JOIN policy_revision r ON r.policy_id=p.id WHERE p.policy_id=$3 AND r.policy_revision_id=$4`, strings.Repeat("a", 64), strings.Repeat("b", 64), initialized.Policy.ID, initialized.Revision.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = old.db.ExecContext(ctx, "INSERT INTO run_submission_credential(run_id,token_hash,created_at) SELECT id,$1,1000 FROM guardrail_run WHERE run_id='run_before_upgrade'", strings.Repeat("c", 64)); err != nil {
		t.Fatal(err)
	}
	if err = old.Close(); err != nil {
		t.Fatal(err)
	}
	if failed, err := openWithMigrations(ctx, config, broken); err == nil {
		failed.Close()
		t.Fatal("broken table rebuild succeeded")
	}
	stale, err := OpenExisting(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	var version, count int
	if err = stale.db.QueryRowContext(ctx, "SELECT MAX(version_id) FROM goose_db_version WHERE is_applied=1").Scan(&version); err != nil || version != 10 {
		t.Fatalf("failed migration advanced version: %d %v", version, err)
	}
	if err = stale.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM guardrail_run WHERE run_id='run_before_upgrade'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("failed migration lost Run: %d %v", count, err)
	}
	if err = stale.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_schema WHERE name='guardrail_run_new'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed migration left temporary table: %d %v", count, err)
	}
	if err = stale.Close(); err != nil {
		t.Fatal(err)
	}
	upgraded, err := Open(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	if err = upgraded.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM run_submission_credential c JOIN guardrail_run r ON r.id=c.run_id WHERE r.run_id='run_before_upgrade'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("upgrade lost Run reference: %d %v", count, err)
	}
	var table, parent string
	var rowID, foreignKey int64
	if err = upgraded.db.QueryRowContext(ctx, "PRAGMA foreign_key_check").Scan(&table, &rowID, &parent, &foreignKey); err != sql.ErrNoRows {
		t.Fatalf("upgrade left invalid foreign keys: %v", err)
	}
}
