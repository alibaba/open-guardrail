package storage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/alibaba/open-guardrail/internal/application/capability"
	"github.com/alibaba/open-guardrail/internal/config"
)

func testBackend(t *testing.T, c config.Config, expected capability.Storage) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := Open(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Ready(ctx); err != nil {
		t.Fatal(err)
	}
	if actual := s.StorageCapability(); actual != expected {
		t.Fatalf("storage capability: got %+v want %+v", actual, expected)
	}
	// A persisted fixture verifies reopening; this table exists only in tests.
	if _, err = s.db.ExecContext(ctx, "CREATE TABLE bootstrap_test (value INTEGER NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.ExecContext(ctx, "INSERT INTO bootstrap_test VALUES (42)"); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	if err = s.Ready(ctx); err == nil {
		t.Fatal("closed database reported ready")
	}
	s, err = Open(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var value int
	if err = s.db.QueryRowContext(ctx, "SELECT value FROM bootstrap_test").Scan(&value); err != nil || value != 42 {
		t.Fatalf("persistence: %d %v", value, err)
	}
	if _, err = s.db.ExecContext(ctx, "DROP TABLE bootstrap_test"); err != nil {
		t.Fatal(err)
	}
	// Tampered/newer migration state must fail readiness and startup.
	definition, _ := lookupDriver(c.Database.Driver)
	versionTable := definition.versionTable
	if versionTable == "" {
		versionTable = "goose_db_version"
	}
	if _, err = s.db.ExecContext(ctx, "INSERT INTO "+versionTable+" (version_id, is_applied) VALUES (999, true)"); err != nil {
		t.Fatal(err)
	}
	if s.Ready(ctx) == nil {
		t.Fatal("newer schema reported ready")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	if newer, err := Open(ctx, c); err == nil {
		newer.Close()
		t.Fatal("newer schema accepted")
	}
}
func testConfig(t *testing.T, driver string) config.Config {
	t.Helper()
	switch driver {
	case "sqlite":
		return config.Config{DataDir: t.TempDir(), Database: config.Database{Driver: "sqlite", MaxConnections: 1}}
	case "postgres":
		return config.Config{Database: config.Database{Driver: "postgres", URL: isolatedPostgresDSN(t), MaxConnections: 2}}
	default:
		if factory, ok := testConfigFactories[driver]; ok {
			return factory(t)
		}
		t.Fatalf("registered driver %q has no conformance fixture", driver)
		return config.Config{}
	}
}

func TestOfficialDriverLifecycle(t *testing.T) {
	for _, driver := range registeredDrivers() {
		t.Run(driver.name, func(t *testing.T) {
			testBackend(t, testConfig(t, driver.name), driver.capability)
		})
	}
}

func TestGeneratedPrimaryKeysAndBusinessUniqueness(t *testing.T) {
	for _, driver := range registeredDrivers() {
		t.Run(driver.name, func(t *testing.T) {
			store := identityStore(t, driver.name)
			if _, err := store.db.Exec("INSERT INTO workspace(workspace_id,name) VALUES('unique','Unique')"); err != nil {
				t.Fatal(err)
			}
			var id int64
			if err := store.db.QueryRow("SELECT id FROM workspace WHERE workspace_id='unique'").Scan(&id); err != nil || id < 1 {
				t.Fatalf("generated key: %d %v", id, err)
			}
			if _, err := store.db.Exec("INSERT INTO workspace(workspace_id,name) VALUES('unique','Duplicate')"); err == nil {
				t.Fatal("duplicate business identifier accepted")
			}
			if _, err := store.db.Exec("INSERT INTO audit_event(event_type,actor_id,occurred_at) VALUES('test','test',0)"); err != nil {
				t.Fatal(err)
			}
			if err := store.db.QueryRow("SELECT id FROM audit_event").Scan(&id); err != nil || id < 1 {
				t.Fatalf("generated audit key: %d %v", id, err)
			}
		})
	}
}

func TestSQLitePrimaryKeySchema(t *testing.T) {
	store := identityStore(t, "sqlite")
	ctx := context.Background()
	for table, column := range map[string]string{
		"identity_state":            "id",
		"workspace":                 "id",
		"project":                   "id",
		"local_user":                "id",
		"admin_session":             "id",
		"project_api_key":           "id",
		"audit_event":               "id",
		"scene":                     "id",
		"idempotency_record":        "id",
		"policy":                    "id",
		"policy_revision":           "id",
		"guardrail_run":             "id",
		"submission":                "id",
		"evidence":                  "id",
		"run_submission_credential": "id",
	} {
		var count int
		err := store.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ? AND "notnull" = 1 AND pk = 1`,
			table, column).Scan(&count)
		if err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("%s.%s must be a NOT NULL primary key", table, column)
		}
	}
	var definition string
	if err := store.db.QueryRowContext(ctx,
		"SELECT sql FROM sqlite_schema WHERE type = 'table' AND name = 'identity_state'",
	).Scan(&definition); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToUpper(definition), "WITHOUT ROWID") {
		t.Fatal("identity_state must be WITHOUT ROWID")
	}
}

func TestMigrationFailureRollsBack(t *testing.T) {
	c := config.Config{DataDir: t.TempDir(), Database: config.Database{Driver: "sqlite"}}
	files := fstest.MapFS{
		"sqlite/00001_broken.sql": {Data: []byte("-- +goose Up\nCREATE TABLE should_rollback (value INTEGER);\nTHIS IS INVALID SQL;\n")},
	}
	if s, err := openWithMigrations(context.Background(), c, files); err == nil {
		s.Close()
		t.Fatal("invalid migration succeeded")
	}
	s, err := Open(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name = 'should_rollback'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("migration did not roll back: %d %v", count, err)
	}
}
func TestSQLiteInvalidDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file")
	if err := os.WriteFile(path, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	if s, err := Open(context.Background(), config.Config{DataDir: path, Database: config.Database{Driver: "sqlite"}}); err == nil {
		s.Close()
		t.Fatal("expected startup failure")
	}
}
func TestPostgresFailureNeverFallsBack(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	dir := filepath.Join(t.TempDir(), "must-not-exist")
	s, err := Open(ctx, config.Config{DataDir: dir, Database: config.Database{Driver: "postgres", URL: "postgres://localhost:1/invalid?connect_timeout=1", MaxConnections: 1}})
	if err == nil {
		s.Close()
		t.Fatal("expected connection failure")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("created SQLite fallback data")
	}
}

func TestDriverConfigurationFailsBeforeOpeningConnection(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config config.Config
	}{
		{"unknown", config.Config{Database: config.Database{Driver: "unknown"}}},
		{"postgres without URL", config.Config{Database: config.Database{Driver: "postgres"}}},
		{"SQLite with URL", config.Config{DataDir: t.TempDir(), Database: config.Database{Driver: "sqlite", URL: "postgres://must-not-connect"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if store, err := Open(context.Background(), tc.config); err == nil {
				store.Close()
				t.Fatal("invalid driver configuration succeeded")
			}
		})
	}
}

var testConfigFactories = map[string]func(*testing.T) config.Config{}
