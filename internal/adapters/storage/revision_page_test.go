package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alibaba/open-guardrail/internal/application/spec"
)

func TestRevisionPagination(t *testing.T) {
	for _, backend := range registeredDrivers() {
		t.Run(backend.name, func(t *testing.T) {
			store := identityStore(t, backend.name)
			ctx := context.Background()
			scope := spec.Scope{WorkspaceID: "w", ProjectID: "p", ActorID: "admin"}
			if _, err := store.db.ExecContext(ctx, "INSERT INTO workspace(workspace_id,name) VALUES('w','w')"); err != nil {
				t.Fatal(err)
			}
			if _, err := store.db.ExecContext(ctx, "INSERT INTO project(project_id,workspace_id,name) VALUES('p',(SELECT id FROM workspace WHERE workspace_id='w'),'p')"); err != nil {
				t.Fatal(err)
			}
			initialized, _, err := spec.New(store.Specs()).InitializeScene(ctx, scope, "recovery", "Recovery", "default", sampleSpec, "init")
			if err != nil {
				t.Fatal(err)
			}
			id := initialized.Policy.ID
			for i := 2; i <= 8; i++ {
				if _, _, err := spec.New(store.Specs()).PublishPolicyRevision(ctx, scope, id, sampleSpec, fmt.Sprint("publish-", i), false); err != nil {
					t.Fatal(err)
				}
			}
			before := int64(0)
			expected := int64(8)
			for pageNo := 0; pageNo < 3; pageNo++ {
				page, err := store.Specs().ListPolicyRevisions(ctx, scope, id, 3, before)
				if err != nil {
					t.Fatal(err)
				}
				if len(page.Items) == 0 || len(page.Items) > 3 {
					t.Fatalf("bad page: %+v", page)
				}
				raw, _ := json.Marshal(page)
				if strings.Contains(string(raw), `"yaml"`) {
					t.Fatal("summary page includes YAML")
				}
				for _, item := range page.Items {
					if item.RevisionNumber != expected {
						t.Fatalf("revision %d, want %d", item.RevisionNumber, expected)
					}
					expected--
				}
				if pageNo == 0 {
					if _, _, err := spec.New(store.Specs()).PublishPolicyRevision(ctx, scope, id, sampleSpec, "new-publication", false); err != nil {
						t.Fatal(err)
					}
				}
				if page.NextBefore == nil {
					if expected != 0 {
						t.Fatal("history truncated")
					}
					break
				}
				before = *page.NextBefore
			}
			if expected != 0 {
				t.Fatal("missing history")
			}
			wrong := scope
			wrong.ProjectID = "other"
			if _, err := store.Specs().ListPolicyRevisions(ctx, wrong, id, 20, 0); !errors.Is(err, spec.ErrNotFound) {
				t.Fatalf("scope leak: %v", err)
			}
			for _, size := range []int{0, 101} {
				if _, err := store.Specs().ListPolicyRevisions(ctx, scope, id, size, 0); !errors.Is(err, spec.ErrInvalid) {
					t.Fatal("invalid page size accepted")
				}
			}
		})
	}
}

func TestPolicyReadCancelsWhileWaitingForConnection(t *testing.T) {
	for _, backend := range registeredDrivers() {
		t.Run(backend.name, func(t *testing.T) {
			store := identityStore(t, backend.name)
			store.db.SetMaxOpenConns(1)
			held, err := store.db.Conn(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer held.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			_, err = store.Specs().GetPolicy(ctx, spec.Scope{WorkspaceID: "w", ProjectID: "p"}, "any")
			if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, spec.ErrUnavailable) {
				t.Fatalf("deadline cause lost: %v", err)
			}
		})
	}
}

func TestPolicyWriteCancelsWhileWaitingForLock(t *testing.T) {
	for _, backend := range registeredDrivers() {
		t.Run(backend.name, func(t *testing.T) {
			store := identityStore(t, backend.name)
			ctx := context.Background()
			scope := spec.Scope{WorkspaceID: "w", ProjectID: "p", ActorID: "admin"}
			if _, err := store.db.ExecContext(ctx, "INSERT INTO workspace(workspace_id,name) VALUES('w','w')"); err != nil {
				t.Fatal(err)
			}
			if _, err := store.db.ExecContext(ctx, "INSERT INTO project(project_id,workspace_id,name) VALUES('p',(SELECT id FROM workspace WHERE workspace_id='w'),'p')"); err != nil {
				t.Fatal(err)
			}
			initialized, _, err := spec.New(store.Specs()).InitializeScene(ctx, scope, "recovery", "Recovery", "default", sampleSpec, "init")
			if err != nil {
				t.Fatal(err)
			}
			lock, err := store.db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Rollback()
			if _, err := lock.ExecContext(ctx, "UPDATE policy SET lifecycle_state=lifecycle_state WHERE policy_id=$1", initialized.Policy.ID); err != nil {
				t.Fatal(err)
			}
			// SQLite's single writer uses the same request budget for connection waits;
			// network backends exercise a real concurrent row-lock wait here.
			if backend.name == "sqlite" {
				store.db.SetMaxOpenConns(1)
			} else {
				store.db.SetMaxOpenConns(2)
			}
			limited, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
			defer cancel()
			_, err = store.Specs().SavePolicyDraft(limited, scope, initialized.Policy.ID, "must not persist")
			if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, spec.ErrUnavailable) {
				t.Fatalf("lock wait did not retain deadline cause: %v", err)
			}
			if err := lock.Rollback(); err != nil {
				t.Fatal(err)
			}
			_, err = store.Specs().GetPolicyDraft(ctx, scope, initialized.Policy.ID)
			if !errors.Is(err, spec.ErrNotFound) {
				t.Fatalf("canceled write created a draft: %v", err)
			}
		})
	}
}
