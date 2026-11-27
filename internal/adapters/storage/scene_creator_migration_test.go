package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/alibaba/open-guardrail/internal/application/spec"
	"github.com/alibaba/open-guardrail/migrations"
)

func TestSceneCreatorMigrationAndAuthenticatedCreation(t *testing.T) {
	ctx := context.Background()
	c := testConfig(t, "sqlite")
	previous := fstest.MapFS{}
	entries, err := migrations.Files.ReadDir("sqlite")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() >= "00010" {
			continue
		}
		name := "sqlite/" + entry.Name()
		data, err := migrations.Files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		previous[name] = &fstest.MapFile{Data: data}
	}
	old, err := openWithMigrations(ctx, c, previous)
	if err != nil {
		t.Fatal(err)
	}
	_, err = old.db.ExecContext(ctx, "INSERT INTO scene(workspace_id,project_id,scene_key,name) SELECT workspace_id,id,'history','History' FROM project WHERE project_id='default'")
	if err != nil {
		t.Fatal(err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	upgraded, err := Open(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	var creator sql.NullString
	if err := upgraded.db.QueryRowContext(ctx, "SELECT created_by FROM scene WHERE scene_key='history'").Scan(&creator); err != nil || creator.Valid {
		t.Fatalf("historical creator must remain SQL NULL: %+v %v", creator, err)
	}
	service := spec.New(upgraded.Specs())
	scope := spec.Scope{WorkspaceID: "default", ProjectID: "default", ActorID: "alice"}
	scene, err := service.GetSceneModel(ctx, scope, "history")
	if err != nil || scene.CreatedBy != nil {
		t.Fatalf("unknown creator changed: %+v %v", scene, err)
	}
	raw, _ := json.Marshal(scene)
	if !strings.Contains(string(raw), `"createdBy":null`) {
		t.Fatalf("unknown creator must be JSON null: %s", raw)
	}
	ttl := int64(3600)
	if _, err := service.UpdateScene(ctx, scope, "history", &ttl, nil); err != nil {
		t.Fatal(err)
	}
	if err := upgraded.db.QueryRowContext(ctx, "SELECT created_by FROM scene WHERE scene_key='history'").Scan(&creator); err != nil || creator.Valid {
		t.Fatalf("edit claimed historical ownership: %+v %v", creator, err)
	}
	created, _, err := service.InitializeScene(ctx, scope, "new", "New", "Default", sampleSpec, "new-scene")
	if err != nil || created.Scene.CreatedBy == nil || *created.Scene.CreatedBy != "alice" {
		t.Fatalf("new Scene lost authenticated creator: %+v %v", created, err)
	}
	scope.ActorID = "bob"
	replay, fresh, err := service.InitializeScene(ctx, scope, "new", "New", "Default", sampleSpec, "new-scene")
	if err != nil || fresh || replay.Scene.CreatedBy == nil || *replay.Scene.CreatedBy != "alice" {
		t.Fatalf("retry reassigned creator: %+v fresh=%v err=%v", replay, fresh, err)
	}
	scope.ActorID = ""
	if _, _, err := service.InitializeScene(ctx, scope, "anonymous", "Anonymous", "Default", sampleSpec, "anonymous"); !errors.Is(err, spec.ErrInvalid) {
		t.Fatalf("missing identity accepted: %v", err)
	}
}
