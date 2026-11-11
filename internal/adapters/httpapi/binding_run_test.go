package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alibaba/open-guardrail/internal/adapters/security"
	"github.com/alibaba/open-guardrail/internal/adapters/storage"
	"github.com/alibaba/open-guardrail/internal/application/identity"
	runapp "github.com/alibaba/open-guardrail/internal/application/run"
	"github.com/alibaba/open-guardrail/internal/application/spec"
	"github.com/alibaba/open-guardrail/internal/config"
	"github.com/alibaba/open-guardrail/internal/domain/resource"
	rundomain "github.com/alibaba/open-guardrail/internal/domain/run"
)

func TestCallerResourceBindingsAndPrivilegedOverride(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, config.Config{DataDir: t.TempDir(), Database: config.Database{Driver: "sqlite"}})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ids := identity.New(store.Identity(), security.Passwords{})
	if _, err := ids.BootstrapLocal(ctx, identity.BootstrapRequest{Username: "admin", Password: "synthetic-password"}); err != nil {
		t.Fatal(err)
	}
	login, err := ids.Login(ctx, identity.LoginRequest{Username: "admin", Password: "synthetic-password", Kind: "cli"})
	if err != nil {
		t.Fatal(err)
	}
	ordinary, err := ids.CreateAPIKey(ctx, login.Token, "cli", "RUN_ORCHESTRATOR")
	if err != nil {
		t.Fatal(err)
	}
	override, err := ids.CreateAPIKey(ctx, login.Token, "cli", "RUN_ORCHESTRATOR", identity.ScopeRunsCreateWithRevision)
	if err != nil {
		t.Fatal(err)
	}
	specs := spec.New(store.Specs())
	scope := spec.Scope{WorkspaceID: "default", ProjectID: "default", ActorID: "admin"}
	initial, _, err := specs.InitializeScene(ctx, scope, "recovery", "Recovery", "default", sampleHTTPSpec, "init")
	if err != nil {
		t.Fatal(err)
	}
	p, _, err := specs.CreatePolicy(ctx, scope, "recovery", "bound", "policy")
	if err != nil {
		t.Fatal(err)
	}
	rev, _, err := specs.PublishPolicyRevision(ctx, scope, p.ID, sampleHTTPSpec, "publish", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := specs.ActivatePolicyRevision(ctx, scope, p.ID, rev.ID, nil); err != nil {
		t.Fatal(err)
	}
	ref := resource.Reference{Kind: "application", ID: "known"}
	if _, err := specs.PutBinding(ctx, scope, "recovery", spec.Binding{Resource: ref, PolicyID: p.ID}); err != nil {
		t.Fatal(err)
	}
	runs := runapp.New(store.Runs())
	handler := New(store, Options{Identity: ids, ModelSpecs: specs, ModelRuns: runs, Origin: "https://guardrail.example"})
	call := func(key, token string, body map[string]any) *httptest.ResponseRecorder {
		t.Helper()
		data, _ := json.Marshal(body)
		r := httptest.NewRequest("POST", "https://guardrail.example/runtime/v1alpha1/runs", bytes.NewReader(data))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Idempotency-Key", strings.ReplaceAll(t.Name(), "/", "-")+"-"+key)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	base := func() map[string]any {
		return map[string]any{"sceneKey": "recovery", "changeSet": map[string]string{"changeSetRef": "example"}}
	}
	expectPolicy := func(w *httptest.ResponseRecorder, policy string) {
		t.Helper()
		var created struct {
			Run map[string]json.RawMessage `json:"run"`
		}
		if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &created) != nil {
			t.Fatalf("selection: %d %s", w.Code, w.Body.String())
		}
		var selectedPolicy string
		_, hasSelection := created.Run["selection"]
		if json.Unmarshal(created.Run["policyId"], &selectedPolicy) != nil || selectedPolicy != policy || hasSelection != (policy == p.ID) {
			t.Fatalf("selection: %d %s", w.Code, w.Body.String())
		}
	}
	body := base()
	body["resources"] = []resource.Reference{ref}
	expectPolicy(call("bound", ordinary.APIKey, body), p.ID)
	expectPolicy(call("omitted", ordinary.APIKey, base()), initial.Policy.ID)
	body["resources"] = []resource.Reference{{Kind: "application", ID: "unbound"}}
	expectPolicy(call("unbound", ordinary.APIKey, body), initial.Policy.ID)

	// Legal ChangeSet and reference limits must also fit the HTTP envelope.
	refs := make([]resource.Reference, resource.MaxResources)
	for i := range refs {
		refs[i] = resource.Reference{Kind: strings.Repeat("a", 64), ID: fmt.Sprintf("%03d", i) + strings.Repeat("&", 125)}
	}
	body = base()
	body["changeSet"] = map[string]string{"payload": strings.Repeat("x", 250<<10)}
	body["resources"] = refs
	expectPolicy(call("large-context", ordinary.APIKey, body), initial.Policy.ID)
	body["changeSet"] = map[string]string{"payload": strings.Repeat("x", rundomain.MaxChangeSetBytes)}
	if w := call("oversized-context", ordinary.APIKey, body); w.Code != 400 {
		t.Fatalf("oversized ChangeSet accepted: %d", w.Code)
	}

	for _, refs := range [][]resource.Reference{{ref, ref}, {{Kind: "Invalid", ID: "x"}}} {
		body = base()
		body["resources"] = refs
		if w := call("invalid-resources", ordinary.APIKey, body); w.Code != 400 {
			t.Fatalf("invalid resources accepted: %d", w.Code)
		}
	}
	body = base()
	body["policyRevisionRef"] = map[string]string{"policyRevisionId": initial.Revision.ID}
	if w := call("denied-override", ordinary.APIKey, body); w.Code != 403 {
		t.Fatalf("ordinary key bypassed binding: %d", w.Code)
	}
	expectPolicy(call("trusted-override", override.APIKey, body), initial.Policy.ID)
	body["resources"] = []resource.Reference{ref}
	if w := call("mixed-selection", override.APIKey, body); w.Code != 400 {
		t.Fatalf("resource and explicit selection combined: %d", w.Code)
	}
}
