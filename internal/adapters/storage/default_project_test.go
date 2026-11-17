package storage

import (
	"context"
	"sync"
	"testing"
)

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
			if state, err := store.Identity().Status(ctx); err != nil || state.Initialized {
				t.Fatalf("default project initialized an administrator: %v", err)
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
			for _, mode := range []string{"auto", "external"} {
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
			for _, mode := range []string{"auto", "external"} {
				t.Run(mode, func(t *testing.T) {
					ctx := context.Background()
					c := testConfig(t, driver.name)
					store, err := Open(ctx, c)
					if err != nil {
						t.Fatal(err)
					}
					defer store.Close()
					for _, query := range []string{"DROP TABLE identity_state", "DELETE FROM project", "DELETE FROM workspace"} {
						if _, err := store.db.ExecContext(ctx, query); err != nil {
							t.Fatal(err)
						}
					}
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
					if _, err := store.db.ExecContext(ctx, "DELETE FROM project"); err != nil {
						t.Fatal(err)
					}
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
