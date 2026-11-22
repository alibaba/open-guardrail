package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alibaba/open-guardrail/internal/adapters/storage"
	"github.com/alibaba/open-guardrail/internal/application/spec"
	"github.com/alibaba/open-guardrail/internal/config"
)

func TestTrustedOperatorSeparatesSpecMaintainers(t *testing.T) {
	store, err := storage.Open(context.Background(), config.Config{DataDir: t.TempDir(), Database: config.Database{Driver: "sqlite"}})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	handler := New(store, Options{TrustedOperatorNamespace: "example", Origin: "https://guardrail.example", ModelSpecs: spec.New(store.Specs())})
	call := func(method, path, user string, body any, edit func(*http.Request)) *httptest.ResponseRecorder {
		t.Helper()
		var data []byte
		if body != nil {
			data, _ = json.Marshal(body)
			var fields map[string]any
			json.Unmarshal(data, &fields)
			fields["operator"] = user
			data, _ = json.Marshal(fields)
		}
		request := httptest.NewRequest(method, "https://guardrail.example"+path, bytes.NewReader(data))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Guardrail-CSRF", "1")
		if body == nil {
			request.Header.Set("X-Operator", user)
		}
		request.Header.Set("Idempotency-Key", strings.ReplaceAll(user+path, "/", "-"))
		if edit != nil {
			edit(request)
		}
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, request)
		return out
	}
	expect := func(out *httptest.ResponseRecorder, status int) {
		t.Helper()
		if out.Code != status {
			t.Fatalf("got %d want %d: %s", out.Code, status, out.Body.String())
		}
	}
	for _, user := range []string{"", " bad ", "a:b", strings.Repeat("1", 65)} {
		expect(call("GET", "/api/v1alpha1/scenes", user, nil, nil), 400)
	}
	// No caller registry or token verification: any valid supplied operator is trusted.
	expect(call("GET", "/api/v1alpha1/scenes", "other-user", nil, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer ignored")
		r.Header.Set("identity-account-name", "not-registered")
		r.Header.Set("identity-account-token", "not-verified")
	}), 200)
	expect(call("GET", "/api/v1alpha1/scenes", "100001", nil, func(r *http.Request) { r.Header.Add("X-Operator", "100002") }), 400)
	expect(call("POST", "/api/v1alpha1/scenes/recovery:initialize", "100001", map[string]string{"name": "Recovery"}, func(r *http.Request) { r.Header.Set("X-Operator", "100002") }), 400)
	expect(call("POST", "/api/v1alpha1/scenes/recovery:initialize", "100001", map[string]string{"name": "Recovery"}, func(r *http.Request) {
		r.Body = io.NopCloser(strings.NewReader(`{"operator":"100001","operator":"100002"}`))
	}), 400)
	expect(call("GET", "/api/v1alpha1/auth/session", "100001", nil, nil), 404)
	capabilities := call("GET", "/api/v1alpha1/meta/capabilities", "", nil, nil)
	expect(capabilities, 200)
	if !strings.Contains(capabilities.Body.String(), `"trusted-operator"`) || strings.Contains(capabilities.Body.String(), `"external-identity"`) || strings.Contains(capabilities.Body.String(), `"local-administrator"`) {
		t.Fatal("incorrect access mode advertised")
	}

	initialized := call("POST", "/api/v1alpha1/scenes/recovery:initialize", "100001", map[string]string{"name": "Recovery", "defaultPolicyName": "Default", "yaml": sampleHTTPSpec}, nil)
	expect(initialized, 201)
	var scene spec.InitializedScene
	if json.Unmarshal(initialized.Body.Bytes(), &scene) != nil || !scene.Policy.CanMaintain("example:100001") || scene.Policy.CanMaintain("example:100002") {
		t.Fatalf("wrong creator: %s", initialized.Body.String())
	}
	created := call("POST", "/api/v1alpha1/scenes/recovery/policies", "100002", map[string]string{"name": "Bob's Spec"}, func(r *http.Request) {
		r.Header.Set("X-Operator", "100002")
		r.Body = io.NopCloser(strings.NewReader(`{"name":"Bob's Spec"}`))
	})
	expect(created, 201)
	var b spec.Policy
	if json.Unmarshal(created.Body.Bytes(), &b) != nil || !b.CanMaintain("example:100002") {
		t.Fatal("second creator not preserved")
	}
	aPath := "/api/v1alpha1/policies/" + scene.Policy.ID
	expect(call("PUT", aPath+"/draft", "100002", map[string]string{"yaml": sampleHTTPSpec}, nil), 403)
	expect(call("POST", aPath+"/revisions", "100002", map[string]string{"yaml": sampleHTTPSpec}, nil), 403)
	expect(call("PUT", aPath+"/active-revision", "100002", map[string]any{"policyRevisionId": scene.Revision.ID, "expectedActiveRevisionId": scene.Revision.ID}, nil), 403)
	expect(call("PUT", aPath+"/maintainers", "100002", map[string]any{"maintainers": []string{"example:100002"}}, nil), 403)
	expect(call("PUT", aPath+"/maintainers", "100001", map[string]any{"maintainers": []string{"example:100001", "example:100002"}}, nil), 200)
	expect(call("PUT", aPath+"/draft", "100002", map[string]string{"yaml": sampleHTTPSpec}, nil), 200)
	expect(call("PUT", aPath+"/maintainers", "100001", map[string]any{"maintainers": []string{"example:100001"}}, nil), 200)
	expect(call("PUT", aPath+"/draft", "100002", map[string]string{"yaml": sampleHTTPSpec}, nil), 403)
	expect(call("PUT", "/api/v1alpha1/policies/"+b.ID+"/draft", "100001", map[string]string{"yaml": sampleHTTPSpec}, nil), 403)
}
