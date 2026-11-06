package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alibaba/open-guardrail/internal/adapters/security"
	"github.com/alibaba/open-guardrail/internal/adapters/storage"
	"github.com/alibaba/open-guardrail/internal/application/identity"
	"github.com/alibaba/open-guardrail/internal/config"
)

func TestLocalSetupBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, origin, peer string
		local              bool
		headers            map[string]string
		status             int
	}{
		{"direct IPv4", "http://127.0.0.1:8080", "127.0.0.1:40000", true, nil, 201},
		{"direct IPv6", "http://[::1]:8080", "[::1]:40000", true, nil, 201},
		{"nonlocal peer", "http://127.0.0.1:8080", "192.0.2.1:40000", true, nil, 401},
		{"malformed peer", "http://127.0.0.1:8080", "localhost", true, nil, 401},
		{"public deployment", "https://guardrail.example", "127.0.0.1:40000", false, nil, 401},
		{"local option off", "http://127.0.0.1:8080", "127.0.0.1:40000", false, nil, 401},
		{"public origin cannot opt in", "https://guardrail.example", "127.0.0.1:40000", true, nil, 401},
		{"forwarded", "http://127.0.0.1:8080", "127.0.0.1:40000", true, map[string]string{"Forwarded": "for=127.0.0.1"}, 401},
		{"forwarded IP", "http://127.0.0.1:8080", "127.0.0.1:40000", true, map[string]string{"X-Forwarded-For": "127.0.0.1"}, 401},
		{"forwarded host", "http://127.0.0.1:8080", "127.0.0.1:40000", true, map[string]string{"X-Forwarded-Host": "127.0.0.1:8080"}, 401},
		{"cross origin", "http://127.0.0.1:8080", "127.0.0.1:40000", true, map[string]string{"Origin": "https://untrusted.example"}, 403},
		{"cross site", "http://127.0.0.1:8080", "127.0.0.1:40000", true, map[string]string{"Sec-Fetch-Site": "cross-site"}, 403},
		{"missing CSRF", "http://127.0.0.1:8080", "127.0.0.1:40000", true, map[string]string{"X-Guardrail-CSRF": ""}, 403},
		{"invalid explicit token", "http://127.0.0.1:8080", "127.0.0.1:40000", true, map[string]string{"Authorization": "Bearer ogb_invalid"}, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, err := storage.Open(context.Background(), config.Config{DataDir: t.TempDir(), Database: config.Database{Driver: "sqlite"}})
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			svc := identity.New(store.Identity(), security.Passwords{})
			handler := New(store, Options{Identity: svc, Origin: tc.origin, LocalBootstrap: tc.local})
			request := func() *http.Request {
				r := httptest.NewRequest("POST", tc.origin+"/api/v1alpha1/bootstrap",
					strings.NewReader(`{"username":"admin","password":"synthetic-password-2026","signIn":true}`))
				r.RemoteAddr = tc.peer
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("Origin", tc.origin)
				r.Header.Set("X-Guardrail-CSRF", "1")
				for k, v := range tc.headers {
					r.Header.Set(k, v)
				}
				return r
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, request())
			if w.Code != tc.status {
				t.Fatalf("got %d want %d", w.Code, tc.status)
			}
			initialized, err := svc.Status(context.Background())
			if err != nil || initialized != (tc.status == 201) {
				t.Fatal("unexpected initialized state")
			}
			if tc.status != 201 {
				return
			}
			if strings.Contains(w.Body.String(), "ogs_") || strings.Contains(w.Body.String(), "ogk_") || strings.Contains(w.Body.String(), "password") {
				t.Fatal("response leaked secret")
			}
			cookies := w.Result().Cookies()
			if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
				t.Fatal("missing safe browser session")
			}
			p, err := svc.Authenticate(context.Background(), cookies[0].Value, "browser")
			if err != nil || p.User.Username != "admin" {
				t.Fatal("automatic sign-in failed")
			}
			w = httptest.NewRecorder()
			handler.ServeHTTP(w, request())
			if w.Code != 409 {
				t.Fatal("repeat initialization accepted")
			}
			status := httptest.NewRequest("GET", tc.origin+"/api/v1alpha1/bootstrap", nil)
			status.RemoteAddr = tc.peer
			w = httptest.NewRecorder()
			handler.ServeHTTP(w, status)
			var state struct{ Initialized, RequiresToken bool }
			if err = json.Unmarshal(w.Body.Bytes(), &state); err != nil || !state.Initialized || state.RequiresToken {
				t.Fatal("incorrect setup mode")
			}
		})
	}
}
