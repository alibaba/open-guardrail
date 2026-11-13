package storage

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/alibaba/open-guardrail/internal/application/audit"
)

func TestAuditPaginationIsolationAndHistoricalDetails(t *testing.T) {
	for _, backend := range registeredDrivers() {
		t.Run(backend.name, func(t *testing.T) {
			store := identityStore(t, backend.name)
			seedBindingScope(t, store)
			ctx := context.Background()
			scope := audit.Scope{WorkspaceID: "default", ProjectID: "default"}
			insert := func(raw any, scoped bool) {
				t.Helper()
				query := `INSERT INTO audit_event(event_type,actor_id,workspace_id,project_id,occurred_at,resource_type,resource_id,details_json) VALUES('test_event','private-actor',(SELECT id FROM workspace WHERE workspace_id=$1),(SELECT id FROM project WHERE project_id=$2),100,'test','private-resource',$3)`
				workspace, project := "default", "default"
				if !scoped {
					workspace, project = "missing", "missing"
				}
				if _, err := store.db.ExecContext(ctx, query, workspace, project, raw); err != nil {
					t.Fatal(err)
				}
			}
			insert(nil, true)
			insert(`{"note":"private-business-context"}`, true)
			insert(`invalid JSON`, true)
			insert(strings.Repeat("x", 4097), true)
			insert(`{"hidden":true}`, false)
			service := audit.New(store.Audits())
			from, to := int64(100), int64(101)
			q := audit.Query{Filters: audit.Filters{EventType: "test_event", ActorID: "private-actor", ResourceType: "test", ResourceID: "private-resource", From: &from, To: &to}, Limit: 2}
			first, err := service.List(ctx, scope, q)
			if err != nil || len(first.Items) != 2 || first.NextCursor == nil {
				t.Fatalf("first=%+v err=%v", first, err)
			}
			if !first.Items[0].DetailsUnavailable || !first.Items[1].DetailsUnavailable || first.Items[1].DetailsDigest == "" {
				t.Fatal("malformed/oversized historical details were not isolated")
			}
			insert(`{"new":true}`, true)
			q.Cursor = *first.NextCursor
			q.Redacted = true
			second, err := service.List(ctx, scope, q)
			if err != nil || len(second.Items) != 2 || second.NextCursor != nil {
				t.Fatalf("second=%+v err=%v", second, err)
			}
			if second.Items[0].ID == first.Items[0].ID || second.Items[0].Details != nil || second.Items[0].ActorID != "" || second.Items[0].ResourceID != nil || len(second.Items[0].ActorRef) != 64 || second.Items[0].DetailsDigest == "" {
				t.Fatal("pagination or redaction lost")
			}
			if second.Items[1].DetailsUnavailable {
				t.Fatal("legacy NULL details must remain readable")
			}
			q.ActorID = "changed"
			if _, err := service.List(ctx, scope, q); !errors.Is(err, audit.ErrInvalid) {
				t.Fatal("cursor reused with changed filters")
			}
			q.ActorID = "private-actor"
			if _, err := service.List(ctx, audit.Scope{WorkspaceID: "default", ProjectID: "other"}, q); !errors.Is(err, audit.ErrInvalid) {
				t.Fatal("cursor reused across project")
			}
			q.Cursor = ""
			empty, err := service.List(ctx, audit.Scope{WorkspaceID: "default", ProjectID: "other"}, q)
			if err != nil || len(empty.Items) != 0 {
				t.Fatal("cross-project audit leak")
			}
			q.From = &to
			if _, err := service.List(ctx, scope, q); !errors.Is(err, audit.ErrInvalid) {
				t.Fatal("invalid interval accepted")
			}
			canceled, cancel := context.WithCancel(ctx)
			cancel()
			q.From = &from
			if _, err := service.List(canceled, scope, q); !errors.Is(err, context.Canceled) || !errors.Is(err, audit.ErrUnavailable) {
				t.Fatalf("cancellation chain: %v", err)
			}
		})
	}
}

func TestAuditDetailByteBoundInDatabase(t *testing.T) {
	for _, backend := range registeredDrivers() {
		t.Run(backend.name, func(t *testing.T) {
			store := identityStore(t, backend.name)
			seedBindingScope(t, store)
			ctx := context.Background()
			cases := []struct {
				name, body  string
				unavailable bool
			}{
				{"ASCII boundary", strings.Repeat("a", audit.MaxDetailsBytes-8), false},
				{"ASCII over", strings.Repeat("a", audit.MaxDetailsBytes-7), true},
				{"Unicode boundary", strings.Repeat("中", 1362) + "aa", false},
				{"Unicode over", strings.Repeat("中", 1362) + "aaa", true},
				{"Unicode large", strings.Repeat("中", 3000), true},
			}
			for _, test := range cases {
				raw := `{"v":"` + test.body + `"}`
				_, err := store.db.ExecContext(ctx, `INSERT INTO audit_event(event_type,actor_id,workspace_id,project_id,occurred_at,resource_id,details_json) VALUES('byte_bound','admin',(SELECT id FROM workspace WHERE workspace_id=$1),(SELECT id FROM project WHERE project_id=$2),100,$3,$4)`, "default", "default", test.name, raw)
				if err != nil {
					t.Fatal(err)
				}
				events, _, err := store.Audits().ListAuditEvents(ctx, audit.Scope{WorkspaceID: "default", ProjectID: "default"}, audit.Filters{EventType: "byte_bound", ResourceID: test.name}, 1, 0)
				if err != nil || len(events) != 1 {
					t.Fatalf("%s: %v %+v", test.name, err, events)
				}
				event := events[0]
				if event.DetailsUnavailable != test.unavailable || (event.Details == nil) != test.unavailable {
					t.Fatalf("%s bytes=%d event=%+v", test.name, len(raw), event)
				}
				if !test.unavailable && string(event.Details) != raw {
					t.Fatalf("%s changed stored detail bytes", test.name)
				}
			}
		})
	}
}
