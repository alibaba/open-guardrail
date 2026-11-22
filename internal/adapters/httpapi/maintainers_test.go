package httpapi

import (
	"context"
	"github.com/alibaba/open-guardrail/internal/adapters/storage"
	"github.com/alibaba/open-guardrail/internal/application/spec"
	"github.com/alibaba/open-guardrail/internal/config"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStandaloneEditsLegacyMaintainedPolicy(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, config.Config{DataDir: t.TempDir(), Database: config.Database{Driver: "sqlite"}})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := spec.New(store.Specs())
	legacy := spec.Scope{WorkspaceID: "default", ProjectID: "default", ActorID: "key:legacy"}
	initial, _, err := svc.InitializeScene(ctx, legacy, "recovery", "Recovery", "default", sampleHTTPSpec, "init")
	if err != nil {
		t.Fatal(err)
	}
	h := New(store, Options{Origin: "https://guardrail.example", ModelSpecs: svc})
	request := httptest.NewRequest("PUT", "https://guardrail.example/api/v1alpha1/policies/"+initial.Policy.ID+"/draft", strings.NewReader(`{"yaml":""}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Guardrail-CSRF", "1")
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("legacy Policy inaccessible: %d %s", response.Code, response.Body.String())
	}
	// Enterprise scopes still enforce the original maintainer list.
	if _, err := svc.SavePolicyDraft(ctx, spec.Scope{WorkspaceID: "default", ProjectID: "default", ActorID: "stranger"}, initial.Policy.ID, ""); err == nil {
		t.Fatal("maintainer boundary removed from enterprise scope")
	}
}
