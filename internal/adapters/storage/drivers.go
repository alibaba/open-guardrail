package storage

import (
	"database/sql"
	"errors"
	"io/fs"
	"sort"

	"github.com/alibaba/open-guardrail/internal/adapters/storage/postgres"
	"github.com/alibaba/open-guardrail/internal/adapters/storage/sqlite"
	"github.com/alibaba/open-guardrail/internal/application/capability"
	"github.com/alibaba/open-guardrail/internal/config"
	"github.com/alibaba/open-guardrail/storage/driver"
	"github.com/pressly/goose/v3"
)

type driverDefinition struct {
	name               string
	migrationDirectory string
	versionTable       string
	migrations         fs.FS
	conflictClause     func(string) string
	isDuplicate        func(error) bool
	dialect            goose.Dialect
	capability         capability.Storage
	validate           func(config.Config) error
	open               func(config.Config) (*sql.DB, error)
	openExisting       func(config.Config) (*sql.DB, error)
}

var driverRegistry = map[string]driverDefinition{
	"sqlite": {
		name:               "sqlite",
		migrationDirectory: "sqlite",
		dialect:            goose.DialectSQLite3,
		capability: capability.Storage{
			SupportLevel:  capability.SupportOfficialLocal,
			MultiInstance: false,
		},
		validate: func(c config.Config) error {
			if c.Database.URL != "" {
				return errors.New("DATABASE_URL requires explicitly selecting a network database driver")
			}
			return nil
		},
		open: func(c config.Config) (*sql.DB, error) {
			return sqlite.Open(c.DataDir)
		},
		openExisting: func(c config.Config) (*sql.DB, error) {
			return sqlite.OpenExisting(c.DataDir)
		},
	},
	"postgres": {
		name:               "postgres",
		migrationDirectory: "postgres",
		dialect:            goose.DialectPostgres,
		capability: capability.Storage{
			SupportLevel: capability.SupportOfficialShared,
			// Multi-instance remains false until every implemented business
			// invariant has a cross-process conformance test.
			MultiInstance: false,
		},
		validate: func(c config.Config) error {
			if c.Database.URL == "" {
				return errors.New("PostgreSQL requires database.url or DATABASE_URL")
			}
			return nil
		},
		open: func(c config.Config) (*sql.DB, error) {
			return postgres.Open(c.Database.URL, c.Database.MaxConnections)
		},
		openExisting: func(c config.Config) (*sql.DB, error) {
			return postgres.Open(c.Database.URL, c.Database.MaxConnections)
		},
	},
}

func registeredDrivers() []driverDefinition {
	drivers := make([]driverDefinition, 0, len(driverRegistry))
	for _, d := range driverRegistry {
		drivers = append(drivers, withConflictDefaults(d))
	}
	for _, d := range driver.Definitions() {
		options := func(c config.Config) driver.Options {
			return driver.Options{DataDir: c.DataDir, URL: c.Database.URL, MaxConnections: c.Database.MaxConnections}
		}
		drivers = append(drivers, driverDefinition{
			name: d.Name, dialect: d.Dialect, migrations: d.Migrations, versionTable: d.VersionTable,
			capability:     capability.Storage{SupportLevel: capability.SupportOfficialShared, MultiInstance: false},
			validate:       func(c config.Config) error { return d.Validate(options(c)) },
			open:           func(c config.Config) (*sql.DB, error) { return d.Open(options(c)) },
			openExisting:   func(c config.Config) (*sql.DB, error) { return d.OpenExisting(options(c)) },
			conflictClause: d.ConflictClause, isDuplicate: d.IsDuplicate,
		})
	}
	sort.Slice(drivers, func(i, j int) bool { return drivers[i].name < drivers[j].name })
	return drivers
}

func withConflictDefaults(d driverDefinition) driverDefinition {
	d.conflictClause = func(columns string) string { return " ON CONFLICT(" + columns + ") DO NOTHING" }
	d.isDuplicate = func(error) bool { return false }
	return d
}

func lookupDriver(name string) (driverDefinition, bool) {
	for _, d := range registeredDrivers() {
		if d.name == name {
			return d, true
		}
	}
	return driverDefinition{}, false
}
