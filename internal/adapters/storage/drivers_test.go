package storage

import (
	"io/fs"
	"regexp"
	"sort"
	"testing"

	"github.com/alibaba/open-guardrail/internal/application/capability"
	"github.com/alibaba/open-guardrail/migrations"
)

var migrationName = regexp.MustCompile(`^(\d+)_.*\.sql$`)

func migrationVersions(t *testing.T, files fs.FS, directory string) []string {
	t.Helper()
	entries, err := fs.ReadDir(files, directory)
	if err != nil {
		t.Fatal(err)
	}
	versions := make([]string, 0, len(entries))
	seen := make(map[string]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		matches := migrationName.FindStringSubmatch(entry.Name())
		if len(matches) != 2 {
			t.Fatalf("invalid migration name for %s: %s", directory, entry.Name())
		}
		version := matches[1]
		if previous, exists := seen[version]; exists {
			t.Fatalf("duplicate migration version %s for %s: %s and %s", version, directory, previous, entry.Name())
		}
		seen[version] = entry.Name()
		versions = append(versions, version)
	}
	sort.Strings(versions)
	if len(versions) == 0 {
		t.Fatalf("driver %s has no migrations", directory)
	}
	return versions
}

func TestOfficialDriverRegistryAndMigrationParity(t *testing.T) {
	drivers := registeredDrivers()
	if len(drivers) == 0 {
		t.Fatal("no official storage driver registered")
	}
	var latest string
	for _, driver := range drivers {
		t.Run(driver.name, func(t *testing.T) {
			registered, ok := lookupDriver(driver.name)
			if !ok || registered.open == nil || registered.openExisting == nil || registered.validate == nil || registered.migrationDirectory == "" && registered.migrations == nil {
				t.Fatalf("incomplete driver registration: %+v", registered)
			}
			if registered.capability.SupportLevel != capability.SupportOfficialLocal &&
				registered.capability.SupportLevel != capability.SupportOfficialShared {
				t.Fatalf("invalid support level: %q", registered.capability.SupportLevel)
			}
			files, directory := fs.FS(migrations.Files), registered.migrationDirectory
			if registered.migrations != nil {
				files, directory = registered.migrations, "."
			}
			versions := migrationVersions(t, files, directory)
			current := versions[len(versions)-1]
			if latest == "" {
				latest = current
				return
			}
			// A newly supported dialect starts from the current schema rather than
			// replaying revisions that were never released for that dialect.
			if current != latest {
				t.Fatalf("latest migration %s does not match %s", current, latest)
			}
		})
	}
}
