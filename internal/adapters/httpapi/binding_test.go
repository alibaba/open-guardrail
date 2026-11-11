package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/alibaba/open-guardrail/internal/adapters/security"
	"github.com/alibaba/open-guardrail/internal/adapters/storage"
	"github.com/alibaba/open-guardrail/internal/application/identity"
	"github.com/alibaba/open-guardrail/internal/application/spec"
	"github.com/alibaba/open-guardrail/internal/config"
)

func TestBindingHTTPAuthenticationAndEncodedResourceID(t *testing.T) {
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
	machine, err := ids.CreateAPIKey(ctx, login.Token, "cli", "RUN_ORCHESTRATOR")
	if err != nil {
		t.Fatal(err)
	}
	specs := spec.New(store.Specs())
	scope := spec.Scope{WorkspaceID: "default", ProjectID: "default", ActorID: "admin"}
	initial, _, err := specs.InitializeScene(ctx, scope, "recovery", "Recovery", "default", sampleHTTPSpec, "init")
	if err != nil {
		t.Fatal(err)
	}
	token := login.Token
	handler := New(store, Options{Identity: ids, Origin: "https://guardrail.example", ModelSpecs: specs})
	call := func(method, path string, auth, csrf bool, body any) *httptest.ResponseRecorder {
		t.Helper()
		data, _ := json.Marshal(body)
		r := httptest.NewRequest(method, "https://guardrail.example"+path, bytes.NewReader(data))
		r.Header.Set("Content-Type", "application/json")
		if auth {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		if csrf {
			r.Header.Set("X-Guardrail-CSRF", "1")
		}
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, r)
		return out
	}
	path := "/api/v1alpha1/scenes/recovery/bindings/repository/team%2Frepo"
	body := map[string]string{"policyId": initial.Policy.ID}
	if out := call("PUT", path, false, true, body); out.Code != 401 {
		t.Fatalf("missing auth: %d", out.Code)
	}
	if out := call("PUT", path, true, false, body); out.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF: %d", out.Code)
	}
	for range 2 {
		out := call("PUT", path, true, true, body)
		if out.Code != 200 || !strings.Contains(out.Body.String(), `"id":"team/repo"`) {
			t.Fatalf("encoded resource: %d %s", out.Code, out.Body.String())
		}
	}
	out := call("GET", "/api/v1alpha1/scenes/recovery/bindings", true, false, nil)
	var page spec.BindingPage
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &page) != nil || len(page.Items) != 1 {
		t.Fatalf("list: %d %s", out.Code, out.Body.String())
	}
	token = machine.APIKey
	if out := call("DELETE", path, true, true, nil); out.Code != 401 {
		t.Fatalf("denied delete: %d", out.Code)
	}
	token = login.Token
	for range 2 {
		if out := call("DELETE", path, true, true, nil); out.Code != 204 {
			t.Fatalf("delete: %d %s", out.Code, out.Body.String())
		}
	}
	// The JSON cursor must round-trip resource IDs expanded by HTML escaping.
	for _, id := range []string{strings.Repeat("&", 128), "z"} {
		path = "/api/v1alpha1/scenes/recovery/bindings/" + strings.Repeat("a", 64) + "/" + url.PathEscape(id)
		if out := call("PUT", path, true, true, body); out.Code != 200 {
			t.Fatalf("cursor fixture: %d %s", out.Code, out.Body.String())
		}
	}
	var first struct {
		Items     []spec.Binding `json:"items"`
		NextAfter string         `json:"nextAfter"`
	}
	out = call("GET", "/api/v1alpha1/scenes/recovery/bindings?limit=1", true, false, nil)
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &first) != nil || len(first.Items) != 1 || first.Items[0].Resource.ID != strings.Repeat("&", 128) || first.NextAfter == "" {
		t.Fatalf("first binding page: %d %s", out.Code, out.Body.String())
	}
	out = call("GET", "/api/v1alpha1/scenes/recovery/bindings?limit=1&after="+url.QueryEscape(first.NextAfter), true, false, nil)
	var last struct {
		Items     []spec.Binding `json:"items"`
		NextAfter string         `json:"nextAfter"`
	}
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &last) != nil || len(last.Items) != 1 || last.Items[0].Resource.ID != "z" || last.NextAfter != "" {
		t.Fatalf("returned cursor rejected or skipped bindings: %d %s", out.Code, out.Body.String())
	}
}
