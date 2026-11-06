package driver

import (
	"database/sql"
	"errors"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/pressly/goose/v3"
)

func validDefinition(name string) Definition {
	open := func(Options) (*sql.DB, error) { return nil, errors.New("fixture connection unavailable") }
	return Definition{Name: name, Dialect: goose.DialectPostgres, Migrations: fstest.MapFS{"00001_initial.sql": &fstest.MapFile{Data: []byte("-- fixture")}}, VersionTable: "test_versions", Validate: func(Options) error { return nil }, Open: open, OpenExisting: open, ConflictClause: func(string) string { return "" }, IsDuplicate: func(error) bool { return false }}
}

func TestRegistrationRejectsIncompleteDefinitionsAndReservedNames(t *testing.T) {
	for _, name := range []string{"sqlite", "postgres", "../outside", "invalid-name", ""} {
		if Register(validDefinition(name)) == nil {
			t.Fatalf("accepted invalid or reserved name %q", name)
		}
	}
	d := validDefinition("missing_migrations")
	d.Migrations = nil
	if Register(d) == nil {
		t.Fatal("accepted extension without migrations")
	}
	d = validDefinition("missing_duplicate")
	d.IsDuplicate = nil
	if Register(d) == nil {
		t.Fatal("accepted extension without conflict handling")
	}
	d = validDefinition("invalid_table")
	d.VersionTable = "versions; DROP TABLE workspace"
	if Register(d) == nil {
		t.Fatal("accepted unsafe version table name")
	}
}

func TestConcurrentRegistrationCannotReplaceAnExistingDriver(t *testing.T) {
	d := validDefinition("concurrent_fixture")
	var wait sync.WaitGroup
	results := make(chan error, 16)
	for range 16 {
		wait.Add(1)
		go func() { defer wait.Done(); results <- Register(d) }()
	}
	wait.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful registrations: %d", successes)
	}
	definitions := Definitions()
	for i := range definitions {
		definitions[i].Name = "mutated"
	}
	for _, current := range Definitions() {
		if current.Name == "concurrent_fixture" {
			return
		}
	}
	t.Fatal("caller mutated registry through returned slice")
}
