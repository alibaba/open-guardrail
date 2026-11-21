package storage

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/alibaba/open-guardrail/internal/application/spec"
)

func TestSpecMaintainersControlEveryWrite(t *testing.T) {
	for _, backend := range registeredDrivers() {
		t.Run(backend.name, func(t *testing.T) {
			store := identityStore(t, backend.name)
			ctx := context.Background()
			owner := spec.Scope{WorkspaceID: "default", ProjectID: "default", ActorID: "owner", Operator: "100001"}
			seed := spec.New(store.Specs())
			initial, _, err := seed.InitializeScene(ctx, owner, "recovery", "Recovery", "Default", sampleSpec, "init")
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(initial.Policy.Maintainers, []string{"owner"}) {
				t.Fatal("initializer lost creator")
			}
			policy, _, err := seed.CreatePolicy(ctx, owner, "recovery", "Editable", "new")
			if err != nil {
				t.Fatal(err)
			}
			first, _, err := seed.PublishPolicyRevision(ctx, owner, policy.ID, sampleSpec, "first", false)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := seed.SavePolicyDraft(ctx, owner, policy.ID, sampleSpec); err != nil {
				t.Fatal(err)
			}
			service := spec.New(store.Specs())
			stranger := owner
			stranger.ActorID = "platform-admin"
			attempts := []func() error{
				func() error { _, e := service.SavePolicyDraft(ctx, stranger, policy.ID, "changed"); return e },
				func() error {
					_, _, e := service.PublishPolicyRevision(ctx, stranger, policy.ID, sampleSpec, "forged-direct", false)
					return e
				},
				func() error {
					_, _, e := service.PublishPolicyRevision(ctx, stranger, policy.ID, sampleSpec, "forged-draft", true)
					return e
				},
				func() error {
					_, e := service.ActivatePolicyRevision(ctx, stranger, policy.ID, first.ID, nil)
					return e
				},
				func() error { _, e := service.SetPolicyArchived(ctx, stranger, policy.ID, true); return e },
				func() error {
					_, e := service.SetPolicyMaintainers(ctx, stranger, policy.ID, []string{stranger.ActorID})
					return e
				},
			}
			for i, attempt := range attempts {
				if err := attempt(); !errors.Is(err, spec.ErrForbidden) {
					t.Fatalf("unauthorized operation %d: %v", i, err)
				}
			}
			// A maintainer does not need to be a platform administrator.
			if _, err := service.SetPolicyMaintainers(ctx, owner, policy.ID, []string{"owner", "editor"}); err != nil {
				t.Fatal(err)
			}
			editor := owner
			editor.ActorID = "editor"
			if _, err := service.GetPolicy(ctx, editor, policy.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := service.SavePolicyDraft(ctx, editor, policy.ID, sampleSpec); err != nil {
				t.Fatal(err)
			}
			second, _, err := service.PublishPolicyRevision(ctx, editor, policy.ID, sampleSpec, "second", true)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.ActivatePolicyRevision(ctx, editor, policy.ID, second.ID, nil); err != nil {
				t.Fatal(err)
			}
			if _, err := service.SetPolicyMaintainers(ctx, editor, policy.ID, []string{"editor"}); err != nil {
				t.Fatal(err)
			}
			if _, err := service.SavePolicyDraft(ctx, owner, policy.ID, "revoked"); !errors.Is(err, spec.ErrForbidden) {
				t.Fatalf("revoked writer: %v", err)
			}
			if _, _, err := service.PublishPolicyRevision(ctx, owner, policy.ID, sampleSpec, "first", false); !errors.Is(err, spec.ErrForbidden) {
				t.Fatalf("publication replay bypassed revocation: %v", err)
			}
			if _, err := service.SetPolicyMaintainers(ctx, owner, policy.ID, []string{"owner"}); !errors.Is(err, spec.ErrForbidden) {
				t.Fatalf("revoked maintainer regained control: %v", err)
			}
			for _, ids := range [][]string{nil, {}, {"editor", "editor"}, {"bad user"}} {
				if _, err := service.SetPolicyMaintainers(ctx, editor, policy.ID, ids); !errors.Is(err, spec.ErrInvalid) {
					t.Fatalf("invalid list %v: %v", ids, err)
				}
			}
			var raw string
			if err := store.db.QueryRowContext(ctx, "SELECT details_json FROM audit_event WHERE event_type='policy_maintainers_updated' ORDER BY id DESC LIMIT 1").Scan(&raw); err != nil {
				t.Fatal(err)
			}
			var detail struct{ Before, After []string }
			if json.Unmarshal([]byte(raw), &detail) != nil || !slices.Equal(detail.Before, []string{"editor", "owner"}) || !slices.Equal(detail.After, []string{"editor"}) {
				t.Fatalf("maintainer audit: %s", raw)
			}
			other := editor
			other.ProjectID = "other"
			if _, err := service.SavePolicyDraft(ctx, other, policy.ID, sampleSpec); !errors.Is(err, spec.ErrNotFound) {
				t.Fatalf("cross-project edit: %v", err)
			}
		})
	}
}
