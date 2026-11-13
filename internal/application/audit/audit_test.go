package audit

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type records struct{ items []Event }

func (r records) ListAuditEvents(context.Context, Scope, Filters, int, int64) ([]Event, bool, error) {
	return r.items, false, nil
}
func TestRedactionBoundsHistoricalUnicodeDetails(t *testing.T) {
	resource := "private-resource"
	source := records{items: []Event{{ID: "1", ActorID: "private-actor", ResourceID: &resource, Details: json.RawMessage(`{"value":"` + strings.Repeat("中", 2000) + `"}`)}, {ID: "2", Details: json.RawMessage(`[]`)}}}
	service := New(source)
	scope := Scope{WorkspaceID: "w", ProjectID: "p"}
	page, err := service.List(context.Background(), scope, Query{Limit: 2, Redacted: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range page.Items {
		if e.Details != nil || !e.DetailsUnavailable {
			t.Fatalf("unsafe historical details: %+v", e)
		}
	}
	if source.items[0].ActorID != "private-actor" || source.items[0].Details == nil {
		t.Fatal("redaction mutated repository records")
	}
	for _, q := range []Query{{Limit: 0}, {Limit: 101}, {Limit: 1, Filters: Filters{ActorID: "\xff"}}, {Limit: 1, Filters: Filters{ResourceID: "a\nb"}}, {Limit: 1, Cursor: "broken"}} {
		if _, err := service.List(context.Background(), scope, q); !errors.Is(err, ErrInvalid) {
			t.Fatalf("query accepted: %+v %v", q, err)
		}
	}
}

func TestRedactedAbsentActorHasNoReference(t *testing.T) {
	service := New(records{items: []Event{{ID: "1"}, {ID: "2", ActorID: "admin"}}})
	page, err := service.List(context.Background(), Scope{WorkspaceID: "w", ProjectID: "p"}, Query{Limit: 2, Redacted: true})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(page.Items[0])
	if err != nil || page.Items[0].ActorRef != "" || strings.Contains(string(raw), "actorRef") {
		t.Fatalf("absent actor acquired a reference: %s %v", raw, err)
	}
	if len(page.Items[1].ActorRef) != 64 || page.Items[1].ActorID != "" {
		t.Fatal("recorded actor lost its redacted reference")
	}
}
