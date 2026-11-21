package bootstrap

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/alibaba/open-guardrail/extension/resource"
	"github.com/alibaba/open-guardrail/internal/adapters/storage"
	"github.com/alibaba/open-guardrail/internal/application/run"
	"github.com/alibaba/open-guardrail/internal/application/spec"
	"github.com/alibaba/open-guardrail/internal/config"
)

func TestPluginLookupUsesProjectAndScene(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, config.Config{DataDir: t.TempDir(), Database: config.Database{Driver: "sqlite"}})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	yaml, err := os.ReadFile("../../examples/policies/recovery.yaml")
	if err != nil {
		t.Fatal(err)
	}
	scope := spec.Scope{WorkspaceID: "default", ProjectID: "default", ActorID: "admin"}
	lookup := scenePlugins(store.Specs())
	for key, plugins := range map[string]*resource.Plugins{
		"first":  {BindingCandidates: &resource.Endpoint{URL: "https://first.example.com/resources"}},
		"second": {ChangeSet: &resource.Endpoint{URL: "https://second.example.com/change-set"}},
		"both":   {ChangeSet: &resource.Endpoint{URL: "https://second.example.com/change-set"}, BindingCandidates: &resource.Endpoint{URL: "https://first.example.com/resources"}},
		"empty":  {},
		"plain":  nil,
	} {
		if _, _, err := spec.New(store.Specs()).InitializeSceneWithPlugins(ctx, scope, key, key, "default", string(yaml), key, plugins); err != nil {
			t.Fatal(err)
		}
	}
	for _, key := range []string{"first", "second", "both", "empty", "plain"} {
		got, err := lookup(ctx, run.Scope{WorkspaceID: "default", ProjectID: "default"}, key)
		if err != nil || (got.Resolver != nil) != (key == "first" || key == "both") || (got.Enricher != nil) != (key == "second" || key == "both") {
			t.Fatalf("incorrect plugins for %s: %v", key, err)
		}
	}
	if _, err := lookup(ctx, run.Scope{WorkspaceID: "default", ProjectID: "other"}, "first"); !errors.Is(err, run.ErrNotFound) {
		t.Fatalf("lookup crossed project scope: %v", err)
	}
}
