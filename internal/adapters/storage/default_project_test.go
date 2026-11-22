package storage

import (
	"context"
	"github.com/alibaba/open-guardrail/internal/config"
	"sync"
	"testing"
)

// Managed fixtures supply setup connections independently of application guards.
var defaultProjectResetters = map[string]func(*testing.T, config.Config, bool){}

func defaultProjectModes(c config.Config) []string {
	if c.Database.Migrations == "external" {
		return []string{"external"}
	}
	return []string{"auto", "external"}
}

func resetDefaultProject(t *testing.T, c config.Config, store *Store, workspace bool) {
	t.Helper()
	if reset := defaultProjectResetters[c.Database.Driver]; reset != nil {
		reset(t, c, workspace)
		return
	}
	queries := []string{"DELETE FROM project"}
	if workspace {
		queries = append(queries, "DELETE FROM workspace")
	}
	for _, query := range queries {
		if _, err := store.db.ExecContext(context.Background(), query); err != nil {
			t.Fatal(err)
		}
	}
}

const defaultProjectSQL = "SELECT w.id,p.id,w.name,p.name FROM workspace w JOIN project p ON p.workspace_id=w.id WHERE w.workspace_id='default' AND p.project_id='default'"

func TestDefaultProjectPreparedWithoutAdministrator(t *testing.T) {
	for _, driver := range registeredDrivers() {
		t.Run(driver.name, func(t *testing.T) {
			ctx := context.Background()
			c := testConfig(t, driver.name)
			store, err := Open(ctx, c)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			var workspace, project int64
			var workspaceName, projectName string
			if err := store.db.QueryRowContext(ctx, defaultProjectSQL).Scan(&workspace, &project, &workspaceName, &projectName); err != nil || workspaceName != "Default" || projectName != "Default" {
				t.Fatalf("defaults missing before administrator setup: %v", err)
			}
			var users int
			if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM local_user").Scan(&users); err != nil || users != 0 {
				t.Fatalf("default project initialized a local account: %d %v", users, err)
			}
			for _, query := range []string{
				"UPDATE workspace SET name='Existing workspace' WHERE workspace_id='default'",
				"UPDATE project SET name='Existing project' WHERE project_id='default'",
				"DROP TABLE identity_state",
			} {
				if _, err := store.db.ExecContext(ctx, query); err != nil {
					t.Fatal(err)
				}
			}
			store.Close()
			for _, mode := range defaultProjectModes(c) {
				c.Database.Migrations = mode
				reopened, err := Open(ctx, c)
				if err != nil {
					t.Fatalf("%s startup requires local identity: %v", mode, err)
				}
				var w, p int64
				err = reopened.db.QueryRowContext(ctx, defaultProjectSQL).Scan(&w, &p, &workspaceName, &projectName)
				reopened.Close()
				if err != nil || w != workspace || p != project || workspaceName != "Existing workspace" || projectName != "Existing project" {
					t.Fatalf("%s startup changed existing defaults: %d/%d %q/%q %v", mode, w, p, workspaceName, projectName, err)
				}
			}
		})
	}
}

func TestDefaultProjectRecoversPartialInitialization(t *testing.T) {
	for _, driver := range registeredDrivers() {
		t.Run(driver.name, func(t *testing.T) {
			fixture := testConfig(t, driver.name)
			for _, mode := range defaultProjectModes(fixture) {
				t.Run(mode, func(t *testing.T) {
					ctx := context.Background()
					c := testConfig(t, driver.name)
					store, err := Open(ctx, c)
					if err != nil {
						t.Fatal(err)
					}
					defer store.Close()
					for _, query := range []string{"DROP TABLE identity_state"} {
						if _, err := store.db.ExecContext(ctx, query); err != nil {
							t.Fatal(err)
						}
					}
					resetDefaultProject(t, c, store, true)
					// Exercise concurrent empty initialization without an identity lock.
					var group sync.WaitGroup
					for range 8 {
						group.Go(func() {
							if err := store.ensureDefaultProject(ctx); err != nil {
								t.Error(err)
							}
						})
					}
					group.Wait()
					resetDefaultProject(t, c, store, false)
					store.Close()
					c.Database.Migrations = mode
					reopened, err := Open(ctx, c)
					if err != nil {
						t.Fatalf("partial initialization could not recover: %v", err)
					}
					defer reopened.Close()
					var workspaces, projects int
					err = reopened.db.QueryRowContext(ctx, "SELECT (SELECT COUNT(*) FROM workspace),(SELECT COUNT(*) FROM project)").Scan(&workspaces, &projects)
					if err != nil || workspaces != 1 || projects != 1 {
						t.Fatalf("duplicate or missing defaults: %d/%d %v", workspaces, projects, err)
					}
				})
			}
		})
	}
}
