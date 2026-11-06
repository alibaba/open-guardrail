package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	contract "github.com/alibaba/open-guardrail/api/openapi"
	"github.com/alibaba/open-guardrail/internal/adapters/security"
	"github.com/alibaba/open-guardrail/internal/adapters/storage"
	"github.com/alibaba/open-guardrail/internal/application/identity"
	"github.com/alibaba/open-guardrail/internal/config"
)

func TestCredentialStateErrorIsRejectedAndLogged(t *testing.T) {
	var output bytes.Buffer
	s := &server{options: Options{Logger: slog.New(slog.NewJSONHandler(&output, nil))}}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/runtime/v1alpha1/identity", nil)

	s.identityError(response, request, identity.ErrCredentialState)

	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), `"code":"UNAUTHENTICATED"`) {
		t.Fatalf("unexpected response: status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(output.String(), "stored machine credential state rejected") ||
		!strings.Contains(output.String(), "trace_id") {
		t.Fatalf("missing credential-state diagnostic: %s", output.String())
	}
}

func TestIdentityHTTP(t *testing.T) {
	store, err := storage.Open(context.Background(), config.Config{DataDir: t.TempDir(), Database: config.Database{Driver: "sqlite"}})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := identity.New(store.Identity(), security.Passwords{})
	now := time.Now()
	svc.Now = func() time.Time { return now }
	token := identity.RandomToken("ogb_")
	if err = svc.PrepareBootstrap(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	const origin = "https://guardrail.example"
	handler := New(store, Options{Identity: svc, Origin: origin})
	call := func(method, path, body, bearer string, cookie *http.Cookie, csrf bool, originHeader string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, origin+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		if cookie != nil {
			req.AddCookie(cookie)
		}
		if csrf {
			req.Header.Set("X-Guardrail-CSRF", "1")
		}
		if originHeader != "" {
			req.Header.Set("Origin", originHeader)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	expect := func(w *httptest.ResponseRecorder, status int) {
		t.Helper()
		if w.Code != status {
			t.Fatalf("got %d want %d: %s", w.Code, status, w.Body.String())
		}
	}
	expectError := func(w *httptest.ResponseRecorder, status int, code string) {
		t.Helper()
		expect(w, status)
		var response contract.Error
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatalf("invalid error response: %v", err)
		}
		if response.Code != code || response.Message == "" || response.Message == response.Code || response.TraceId == "" {
			t.Fatalf("unexpected error response: %+v", response)
		}
		if response.TraceId != w.Header().Get(traceIDHeader) {
			t.Fatalf("trace ID mismatch: body=%q header=%q", response.TraceId, w.Header().Get(traceIDHeader))
		}
	}
	const bootstrapBody = `{"username":"admin","password":"synthetic-password-2026"}`
	expectError(call("POST", "/api/v1alpha1/bootstrap", bootstrapBody, token, nil, false, ""), 403, "CSRF_REQUIRED")
	expectError(call("POST", "/api/v1alpha1/bootstrap", bootstrapBody, token, nil, false, "https://untrusted.example"), 403, "UNTRUSTED_ORIGIN")
	expectError(call("POST", "/api/v1alpha1/bootstrap", bootstrapBody, token, nil, true, "https://untrusted.example"), 403, "UNTRUSTED_ORIGIN")
	badHost := httptest.NewRequest("GET", "http://untrusted.example/api/v1alpha1/bootstrap", nil)
	badResult := httptest.NewRecorder()
	handler.ServeHTTP(badResult, badHost)
	expect(badResult, 403)
	expect(call("POST", "/api/v1alpha1/bootstrap", bootstrapBody, "ogb_invalid", nil, true, origin), 401)
	w := call("POST", "/api/v1alpha1/bootstrap", bootstrapBody, token, nil, true, origin)
	expect(w, 201)
	if strings.Contains(w.Body.String(), "apiKey") || strings.Contains(w.Body.String(), "ogk_") {
		t.Fatal("bootstrap issued a machine secret")
	}
	var boot identity.BootstrapResult
	if err = json.Unmarshal(w.Body.Bytes(), &boot); err != nil {
		t.Fatal(err)
	}
	expect(call("POST", "/api/v1alpha1/bootstrap", bootstrapBody, token, nil, true, ""), 409)
	expect(call("POST", "/api/v1alpha1/auth/api-keys", "{}", "", nil, true, origin), 401)
	expect(call("GET", "/api/v1alpha1/projects", "", "", nil, true, ""), 401)
	expect(call("POST", "/api/v1alpha1/auth/session", `{"username":"admin","password":"synthetic-password-2026","kind":"cli"}`, "", nil, true, origin), 400)
	expectError(call("POST", "/api/v1alpha1/auth/session", `{"username":"admin","password":"synthetic-password-2026","kind":"future"}`, "", nil, true, origin), 400, "INVALID_ARGUMENT")
	expect(call("POST", "/api/v1alpha1/auth/session", bootstrapBody+" {}", "", nil, true, origin), 400)
	expect(call("POST", "/api/v1alpha1/auth/session", `{"password":"`+strings.Repeat("x", 5000)+`"}`, "", nil, true, origin), 400)
	w = call("POST", "/api/v1alpha1/auth/session", bootstrapBody, "", nil, true, origin)
	expect(w, 201)
	if strings.Contains(w.Body.String(), "ogs_") || strings.Contains(w.Body.String(), "password") {
		t.Fatal("browser response leaked credential")
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing session cookie")
	}
	cookie := cookies[0]
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" {
		t.Fatalf("unsafe cookie: %+v", cookie)
	}
	expect(call("POST", "/api/v1alpha1/auth/api-keys", "{}", "", cookie, false, origin), 403)
	expect(call("POST", "/api/v1alpha1/auth/api-keys", "{}", "", cookie, true, "https://untrusted.example"), 403)
	w = call("POST", "/api/v1alpha1/auth/api-keys", "{}", "", cookie, true, origin)
	expect(w, 201)
	var key identity.APIKeyResult
	if err = json.Unmarshal(w.Body.Bytes(), &key); err != nil || key.APIKey == "" || !slices.Equal(key.Scopes, []string{
		identity.ScopeRunsCreate,
		identity.ScopeRunsRead,
		identity.ScopeSubmissionCredentialRotate,
	}) {
		t.Fatal("missing machine key")
	}
	expectError(call("POST", "/api/v1alpha1/auth/api-keys", `{"role":"ADMIN"}`, "", cookie, true, origin), 400, "INVALID_ARGUMENT")
	expectError(call("POST", "/api/v1alpha1/auth/api-keys", `{"role":"POLICY_PUBLISHER","scopes":["runs:create-with-revision"]}`, "", cookie, true, origin), 400, "INVALID_ARGUMENT")
	expectError(call("POST", "/api/v1alpha1/auth/api-keys", `{"role":"RUN_ORCHESTRATOR","scopes":["runs:create-with-revision","runs:create-with-revision"]}`, "", cookie, true, origin), 400, "INVALID_ARGUMENT")
	scopedKeyResponse := call("POST", "/api/v1alpha1/auth/api-keys", `{"role":"RUN_ORCHESTRATOR","scopes":["runs:create-with-revision"]}`, "", cookie, true, origin)
	expect(scopedKeyResponse, 201)
	var scopedKey identity.APIKeyResult
	if err = json.Unmarshal(scopedKeyResponse.Body.Bytes(), &scopedKey); err != nil || scopedKey.APIKey == "" || !slices.Equal(scopedKey.Scopes, []string{
		identity.ScopeRunsCreate,
		identity.ScopeRunsCreateWithRevision,
		identity.ScopeRunsRead,
		identity.ScopeSubmissionCredentialRotate,
	}) {
		t.Fatalf("missing scoped machine key: %s: %v", scopedKeyResponse.Body.String(), err)
	}
	policyKeyResponse := call("POST", "/api/v1alpha1/auth/api-keys", `{"role":"POLICY_PUBLISHER"}`, "", cookie, true, origin)
	expect(policyKeyResponse, 201)
	var policyKey identity.APIKeyResult
	if err = json.Unmarshal(policyKeyResponse.Body.Bytes(), &policyKey); err != nil || policyKey.APIKey == "" || policyKey.Role != "POLICY_PUBLISHER" || len(policyKey.Scopes) != 2 {
		t.Fatal("missing policy sync key")
	}
	expect(call("POST", "/api/v1alpha1/auth/api-keys", "{}", policyKey.APIKey, nil, true, origin), 401)
	expect(call("GET", "/runtime/v1alpha1/identity", "", key.APIKey, nil, true, ""), 200)
	scopedIdentity := call("GET", "/runtime/v1alpha1/identity", "", scopedKey.APIKey, nil, true, "")
	expect(scopedIdentity, 200)
	var scopedPrincipal identity.Principal
	if err = json.Unmarshal(scopedIdentity.Body.Bytes(), &scopedPrincipal); err != nil || !slices.Equal(scopedPrincipal.Scopes, scopedKey.Scopes) {
		t.Fatalf("missing identity scope: %s: %v", scopedIdentity.Body.String(), err)
	}
	expect(call("GET", "/api/v1alpha1/auth/me", "", key.APIKey, nil, true, ""), 401)
	expect(call("POST", "/api/v1alpha1/auth/api-keys", "{}", key.APIKey, nil, true, ""), 401)
	expect(call("GET", "/api/v1alpha1/auth/me", "", "", cookie, false, origin), 200)
	expect(call("DELETE", "/api/v1alpha1/auth/session", "", "", cookie, false, origin), 403)
	expect(call("DELETE", "/api/v1alpha1/auth/session", "", "", cookie, true, origin), 204)
	expect(call("GET", "/api/v1alpha1/auth/me", "", "", cookie, false, origin), 401)
	// Browser credentials cannot be reused as CLI credentials, even with the same secret.
	expect(call("GET", "/api/v1alpha1/auth/me", "", cookie.Value, nil, false, ""), 401)

	// Machine credentials cannot perform administrator actions.
	expectError(call("DELETE", "/api/v1alpha1/auth/session", "", key.APIKey, nil, true, ""), 401, "UNAUTHENTICATED")
	passwordBody := `{"currentPassword":"synthetic-password-2026","newPassword":"replacement-password-2026"}`
	expectError(call("PUT", "/api/v1alpha1/auth/password", passwordBody, key.APIKey, nil, true, ""), 401, "UNAUTHENTICATED")

	// Password changes revoke administrator sessions but do not revoke Agent keys.
	w = call("POST", "/api/v1alpha1/auth/session", bootstrapBody, "", nil, true, origin)
	expect(w, 201)
	passwordCookies := w.Result().Cookies()
	if len(passwordCookies) != 1 {
		t.Fatal("missing password-change session cookie")
	}
	passwordCookie := passwordCookies[0]
	expectError(call("GET", "/runtime/v1alpha1/identity", "", "", passwordCookie, false, origin), 401, "UNAUTHENTICATED")
	expect(call("PUT", "/api/v1alpha1/auth/password", passwordBody, "", passwordCookie, true, origin), 204)
	expectError(call("GET", "/api/v1alpha1/auth/me", "", "", passwordCookie, false, origin), 401, "UNAUTHENTICATED")
	expect(call("GET", "/runtime/v1alpha1/identity", "", key.APIKey, nil, false, ""), 200)

	// Failed passwords trigger temporary backoff, never a permanent account lock.
	const wrongPassword = `{"username":"admin","password":"wrong-password"}`
	for range 5 {
		expectError(call("POST", "/api/v1alpha1/auth/session", wrongPassword, "", nil, true, origin), 401, "UNAUTHENTICATED")
	}
	w = call("POST", "/api/v1alpha1/auth/session", `{"username":"admin","password":"replacement-password-2026"}`, "", nil, true, origin)
	expectError(w, 429, "RATE_LIMITED")
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("missing retry guidance")
	}
	now = now.Add(31 * time.Second)
	w = call("POST", "/api/v1alpha1/auth/session", `{"username":"admin","password":"replacement-password-2026"}`, "", nil, true, origin)
	expect(w, 201)
}
