package storage

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/alibaba/open-guardrail/internal/application/spec"
	"github.com/alibaba/open-guardrail/internal/domain/resource"
)

func TestBindingAuditRecordsOnlyActualChanges(t *testing.T) {
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
			ref := resource.Reference{Kind: "application", ID: "7"}
			b := spec.Binding{Resource: ref, PolicyID: initial.Policy.ID}
			if err := specs.DeleteBinding(ctx, scope, "recovery", ref); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if _, err := specs.PutBinding(ctx, scope, "recovery", b); err != nil {
					t.Fatal(err)
				}
			}
			b.PolicyRevisionID = &initial.Revision.ID
			if _, err := specs.PutBinding(ctx, scope, "recovery", b); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if err := specs.DeleteBinding(ctx, scope, "recovery", ref); err != nil {
					t.Fatal(err)
				}
			}
			rows, err := store.db.QueryContext(ctx, "SELECT event_type,details_json FROM audit_event WHERE resource_type='policy-binding' ORDER BY id")
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			n := 0
			want := []string{"policy_binding_created", "policy_binding_updated", "policy_binding_deleted"}
			for rows.Next() {
				var event, raw string
				if err := rows.Scan(&event, &raw); err != nil {
					t.Fatal(err)
				}
				var details struct {
					SceneKey string        `json:"sceneKey"`
					Before   *spec.Binding `json:"before"`
					After    *spec.Binding `json:"after"`
				}
				if json.Unmarshal([]byte(raw), &details) != nil || n >= len(want) || event != want[n] || details.SceneKey != "recovery" {
					t.Fatalf("invalid event %q %s", event, raw)
				}
				switch n {
				case 0:
					if details.Before != nil || details.After == nil || details.After.Resource != ref || details.After.PolicyID != b.PolicyID || details.After.PolicyRevisionID != nil {
						t.Fatal("creation evidence incomplete")
					}
				case 1:
					if details.Before == nil || details.Before.PolicyRevisionID != nil || details.After == nil || details.After.PolicyRevisionID == nil || *details.After.PolicyRevisionID != initial.Revision.ID {
						t.Fatal("replacement lost before/after selection")
					}
				case 2:
					if details.After != nil || details.Before == nil || details.Before.Resource != ref || details.Before.PolicyRevisionID == nil {
						t.Fatal("deletion lost binding evidence")
					}
				}
				n++
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			if n != 3 {
				t.Fatalf("expected three actual changes, got %d", n)
			}
		})
	}
}
