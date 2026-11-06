package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alibaba/open-guardrail/internal/adapters/security"
	"github.com/alibaba/open-guardrail/internal/adapters/storage"
	"github.com/alibaba/open-guardrail/internal/application/identity"
	"github.com/alibaba/open-guardrail/internal/config"
)

func TestAPIKeyManagementHTTP(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, config.Config{DataDir: t.TempDir(), Database: config.Database{Driver: "sqlite"}})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := identity.New(store.Identity(), security.Passwords{})
	now := time.Now()
	svc.Now = func() time.Time { return now }
	if _, err := svc.BootstrapLocal(ctx, identity.BootstrapRequest{Username: "admin", Password: "synthetic-test-password"}); err != nil {
		t.Fatal(err)
	}
	session, err := svc.Login(ctx, identity.LoginRequest{Username: "admin", Password: "synthetic-test-password", Kind: "cli"})
	if err != nil {
		t.Fatal(err)
	}
	const origin = "https://guardrail.example"
	handler := New(store, Options{Identity: svc, Origin: origin})
	call := func(method, path, body, token string, csrf bool, status int) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, origin+path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", origin)
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		if csrf {
			request.Header.Set("X-Guardrail-CSRF", "1")
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != status {
			t.Fatalf("%s %s: status=%d want=%d", method, path, response.Code, status)
		}
		return response
	}
	const base = "/api/v1alpha1/auth/api-keys"
	call("GET", base, "", "", false, 401)
	call("POST", base, `{"expiresInSeconds":0}`, session.Token, true, 400)
	created := call("POST", base, `{"expiresInSeconds":60}`, session.Token, true, 201)
	var key identity.APIKeyResult
	if err := json.Unmarshal(created.Body.Bytes(), &key); err != nil || key.ID == "" || key.APIKey == "" || key.Version != 1 {
		t.Fatal("invalid key response")
	}
	listed := call("GET", base+"?pageSize=1", "", session.Token, false, 200)
	if strings.Contains(listed.Body.String(), key.APIKey) || strings.Contains(listed.Body.String(), identity.Digest(key.APIKey)) {
		t.Fatal("metadata endpoint exposed credential material")
	}
	call("GET", base+"?pageSize=101", "", session.Token, false, 400)
	call("GET", base, "", key.APIKey, false, 401)
	call("DELETE", base+"/"+key.ID, "", key.APIKey, true, 401)
	call("POST", base+"/"+key.ID+"/rotation", `{"expectedVersion":1}`, session.Token, false, 403)
	call("POST", base+"/"+key.ID+"/rotation", `{}`, session.Token, true, 400)
	call("POST", base+"/"+key.ID+"/rotation", `{"expectedVersion":null}`, session.Token, true, 400)
	call("POST", base+"/"+key.ID+"/rotation", `{"expectedVersion":1}`, key.APIKey, true, 401)
	rotated := call("POST", base+"/"+key.ID+"/rotation", `{"expectedVersion":1}`, session.Token, true, 201)
	var replacement identity.APIKeyResult
	if err := json.Unmarshal(rotated.Body.Bytes(), &replacement); err != nil || replacement.ID != key.ID || replacement.Version != 2 || replacement.APIKey == key.APIKey {
		t.Fatal("invalid rotation response")
	}
	call("POST", base+"/"+key.ID+"/rotation", `{"expectedVersion":1}`, session.Token, true, 409)
	call("GET", "/runtime/v1alpha1/identity", "", key.APIKey, false, 401)
	call("GET", "/runtime/v1alpha1/identity", "", replacement.APIKey, false, 200)
	call("DELETE", base+"/"+key.ID, "", session.Token, false, 403)
	call("DELETE", base+"/"+key.ID, "", session.Token, true, 204)
	call("DELETE", base+"/"+key.ID, "", session.Token, true, 204)
	call("GET", "/runtime/v1alpha1/identity", "", replacement.APIKey, false, 401)
	call("POST", base+"/"+key.ID+"/rotation", `{"expectedVersion":2}`, session.Token, true, 409)
	call("DELETE", base+"/key_"+strings.Repeat("a", 32), "", session.Token, true, 404)
	short := call("POST", base, `{"expiresInSeconds":60}`, session.Token, true, 201)
	var expiring identity.APIKeyResult
	if err := json.Unmarshal(short.Body.Bytes(), &expiring); err != nil {
		t.Fatal(err)
	}
	now = time.Unix(expiring.ExpiresAt, 0)
	call("GET", "/runtime/v1alpha1/identity", "", expiring.APIKey, false, 401)
	call("POST", base+"/"+expiring.ID+"/rotation", `{"expectedVersion":1}`, session.Token, true, 409)
}
