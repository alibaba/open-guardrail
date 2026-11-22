package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/alibaba/open-guardrail/internal/adapters/storage"
	runapp "github.com/alibaba/open-guardrail/internal/application/run"
	"github.com/alibaba/open-guardrail/internal/application/spec"
	"github.com/alibaba/open-guardrail/internal/config"
)

func TestRunScopeEnforcesRequestGuards(t *testing.T) {
	for _, namespace := range []string{"", "example"} {
		name := "standalone"
		if namespace != "" {
			name = "trusted-operator"
		}
		t.Run(name, func(t *testing.T) {
			store, err := storage.Open(context.Background(), config.Config{DataDir: t.TempDir(), Database: config.Database{Driver: "sqlite"}})
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			handler := New(store, Options{Origin: "https://guardrail.example", TrustedOperatorNamespace: namespace,
				ModelSpecs: spec.New(store.Specs()), ModelRuns: runapp.New(store.Runs())})
			requestNo := 0
			call := func(method, path string, body any, edit func(*http.Request)) *httptest.ResponseRecorder {
				t.Helper()
				var data []byte
				if body != nil {
					data, err = json.Marshal(body)
					if err != nil {
						t.Fatal(err)
					}
				}
				request := httptest.NewRequest(method, "https://guardrail.example"+path, bytes.NewReader(data))
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("Origin", "https://guardrail.example")
				if namespace != "" {
					request.Header.Set("X-Operator", "100001")
				}
				if method != http.MethodGet {
					request.Header.Set("X-Guardrail-CSRF", "1")
				}
				requestNo++
				request.Header.Set("Idempotency-Key", "request-guard-"+strconv.Itoa(requestNo))
				if edit != nil {
					edit(request)
				}
				out := httptest.NewRecorder()
				handler.ServeHTTP(out, request)
				return out
			}
			initialized := call(http.MethodPost, "/api/v1alpha1/scenes/recovery:initialize",
				map[string]any{"name": "Recovery", "defaultPolicyName": "Default", "yaml": sampleHTTPSpec}, nil)
			if initialized.Code != http.StatusCreated {
				t.Fatalf("initialize returned %d", initialized.Code)
			}
			body := map[string]any{"sceneKey": "recovery", "changeSet": map[string]any{"target": "app"}}
			created := call(http.MethodPost, "/runtime/v1alpha1/runs", body, nil)
			var creation struct {
				Run struct{ ID string }
			}
			if created.Code != http.StatusCreated || json.Unmarshal(created.Body.Bytes(), &creation) != nil || creation.Run.ID == "" {
				t.Fatalf("create Run returned %d without a Run ID", created.Code)
			}
			for _, route := range []struct {
				method, path string
				body         any
				status       int
			}{
				{http.MethodGet, "/runtime/v1alpha1/runs", nil, http.StatusOK},
				{http.MethodGet, "/runtime/v1alpha1/runs/" + creation.Run.ID, nil, http.StatusOK},
				{http.MethodGet, "/runtime/v1alpha1/runs/" + creation.Run.ID + "/policy-revision", nil, http.StatusOK},
				{http.MethodGet, "/api/v1alpha1/runs", nil, http.StatusOK},
				{http.MethodPost, "/runtime/v1alpha1/runs", body, http.StatusCreated},
				{http.MethodPost, "/runtime/v1alpha1/runs/" + creation.Run.ID + "/submission-credential:rotate", nil, http.StatusCreated},
			} {
				t.Run(route.method+" "+route.path, func(t *testing.T) {
					for _, rejection := range []struct {
						name, code string
						writeOnly  bool
						edit       func(*http.Request)
					}{
						{"host", "UNTRUSTED_ORIGIN", false, func(r *http.Request) { r.Host = "other.example" }},
						{"origin", "UNTRUSTED_ORIGIN", false, func(r *http.Request) { r.Header.Set("Origin", "https://other.example") }},
						{"cross-site", "UNTRUSTED_ORIGIN", false, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }},
						{"missing-csrf", "CSRF_REQUIRED", true, func(r *http.Request) { r.Header.Del("X-Guardrail-CSRF") }},
						{"invalid-csrf", "CSRF_REQUIRED", true, func(r *http.Request) { r.Header.Set("X-Guardrail-CSRF", "0") }},
					} {
						if rejection.writeOnly && route.method == http.MethodGet {
							continue
						}
						t.Run(rejection.name, func(t *testing.T) {
							out := call(route.method, route.path, route.body, rejection.edit)
							var response struct{ Code string }
							if out.Code != http.StatusForbidden || json.Unmarshal(out.Body.Bytes(), &response) != nil || response.Code != rejection.code {
								t.Fatalf("rejected request returned %d/%s, want 403/%s", out.Code, response.Code, rejection.code)
							}
						})
					}
					if out := call(route.method, route.path, route.body, nil); out.Code != route.status {
						t.Fatalf("same-origin request returned %d, want %d", out.Code, route.status)
					}
				})
			}
		})
	}
}
