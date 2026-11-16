package storage

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/alibaba/open-guardrail/migrations"
)

func TestAuditIndexUpgradePreservesHistoryAndAvoidsPageSort(t *testing.T) {
	ctx := context.Background()
	config := testConfig(t, "sqlite")
	previous := fstest.MapFS{}
	for _, name := range []string{"00001_initial.sql", "00002_policy_binding.sql", "00003_binding_audit.sql"} {
		raw, err := migrations.Files.ReadFile("sqlite/" + name)
		if err != nil {
			t.Fatal(err)
		}
		previous["sqlite/"+name] = &fstest.MapFile{Data: raw}
	}
	old, err := openWithMigrations(ctx, config, previous)
	if err != nil {
		t.Fatal(err)
	}
	seedBindingScope(t, old)
	if _, err := old.db.ExecContext(ctx, `INSERT INTO audit_event(event_type,actor_id,workspace_id,project_id,occurred_at,details_json) VALUES('legacy','admin',(SELECT id FROM workspace WHERE workspace_id='default'),(SELECT id FROM project WHERE project_id='default'),100,'{"before":"unchanged"}')`); err != nil {
		t.Fatal(err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	if existing, err := OpenExisting(ctx, config); err == nil {
		existing.Close()
		t.Fatal("old externally managed schema was silently accepted")
	}
	upgraded, err := Open(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	var raw string
	if err := upgraded.db.QueryRowContext(ctx, "SELECT details_json FROM audit_event WHERE event_type='legacy'").Scan(&raw); err != nil || raw != `{"before":"unchanged"}` {
		t.Fatalf("upgrade changed audit history: %s %v", raw, err)
	}
	rows, err := upgraded.db.QueryContext(ctx, `EXPLAIN QUERY PLAN SELECT id,event_type,actor_id,occurred_at,resource_type,resource_id,details_json FROM audit_event
 WHERE workspace_id=(SELECT id FROM workspace WHERE workspace_id=$1)
 AND project_id=(SELECT id FROM project WHERE project_id=$2 AND workspace_id=(SELECT id FROM workspace WHERE workspace_id=$3))
 AND id<$4 ORDER BY id DESC LIMIT 21`, "default", "default", "default", 1000)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	text := strings.Join(plan, "\n")
	if !strings.Contains(text, "audit_event_project_id") || strings.Contains(text, "TEMP B-TREE") {
		t.Fatalf("audit page lacks indexed descending access: %s", text)
	}
}
