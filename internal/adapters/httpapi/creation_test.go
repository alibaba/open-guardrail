package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/alibaba/open-guardrail/internal/adapters/storage"
	"github.com/alibaba/open-guardrail/internal/application/spec"
	"github.com/alibaba/open-guardrail/internal/config"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAtomicBoundCreationHTTP(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, config.Config{DataDir: t.TempDir(), Database: config.Database{Driver: "sqlite"}})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := spec.New(store.Specs())
	handler := New(store, Options{Origin: "http://example.com", TrustedOperatorNamespace: "example", ModelSpecs: svc})
	call := func(method, path string, body any, withOperator bool, key string) *httptest.ResponseRecorder {
		var data []byte
		if body != nil {
			data, _ = json.Marshal(body)
		}
		r := httptest.NewRequest(method, path, bytes.NewReader(data))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Guardrail-CSRF", "1")
		r.Header.Set("Idempotency-Key", key)
		if withOperator {
			r.Header.Set("X-Operator", "100001")
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	body := map[string]any{"name": "Authoring", "defaultPolicyName": "default", "yaml": sampleHTTPSpec, "resource": map[string]string{"kind": "repository", "id": "team/repo"}}
	if out := call("POST", "/api/v1alpha1/scenes/authoring:initialize", body, false, "init"); out.Code != 400 {
		t.Fatal(out.Code)
	}
	if out := call("POST", "/api/v1alpha1/scenes/authoring:initialize", body, true, "init"); out.Code != 400 {
		t.Fatalf("initialization accepted a resource binding: %d %s", out.Code, out.Body)
	}
	delete(body, "resource")
	if out := call("POST", "/api/v1alpha1/scenes/authoring:initialize", body, true, "init"); out.Code != 201 {
		t.Fatalf("init: %d %s", out.Code, out.Body)
	}
	if out := call("GET", "/api/v1alpha1/scenes/authoring/bindings", nil, true, ""); out.Code != 200 || !strings.Contains(out.Body.String(), `"items":[]`) {
		t.Fatalf("initialization created a resource binding: %d %s", out.Code, out.Body)
	}
	body = map[string]any{"name": "Application Spec", "specKey": "application", "resource": map[string]string{"kind": "application", "id": "7"}}
	first := call("POST", "/api/v1alpha1/scenes/authoring/policies", body, true, "create")
	if first.Code != 201 {
		t.Fatalf("create: %d %s", first.Code, first.Body)
	}
	if out := call("POST", "/api/v1alpha1/scenes/authoring/policies", body, true, "create"); out.Code != 200 {
		t.Fatalf("retry: %d %s", out.Code, out.Body)
	}
	if out := call("POST", "/api/v1alpha1/scenes/authoring/policies", body, true, "other"); out.Code != 409 {
		t.Fatalf("collision: %d %s", out.Code, out.Body)
	}
	if out := call("GET", "/api/v1alpha1/scenes/authoring/bindings", nil, true, ""); out.Code != 200 || !strings.Contains(out.Body.String(), `"id":"7"`) {
		t.Fatalf("bindings: %d %s", out.Code, out.Body)
	}
}
