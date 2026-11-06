// Package storage owns the database lifecycle and migration readiness.
package storage

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"

	"github.com/alibaba/open-guardrail/internal/application/capability"
	"github.com/alibaba/open-guardrail/internal/config"
	"github.com/alibaba/open-guardrail/migrations"
	"github.com/pressly/goose/v3"
)

type Store struct {
	db       *sql.DB
	provider *goose.Provider
	expected int64
	storage  capability.Storage
	driver   driverDefinition
}

func Open(ctx context.Context, c config.Config) (*Store, error) {
	if c.Database.Migrations == "external" {
		return OpenExisting(ctx, c)
	}
	return openWithMigrations(ctx, c, migrations.Files)
}

func openWithMigrations(ctx context.Context, c config.Config, migrationFiles fs.FS) (*Store, error) {
	return openDatabase(ctx, c, migrationFiles, false)
}

// OpenExisting opens a schema maintained outside the process. It neither initializes
// nor migrates storage and rejects schemas that are missing or at the wrong version.
func OpenExisting(ctx context.Context, c config.Config) (*Store, error) {
	return openDatabase(ctx, c, migrations.Files, true)
}

func openDatabase(ctx context.Context, c config.Config, migrationFiles fs.FS, existing bool) (*Store, error) {
	driver, ok := lookupDriver(c.Database.Driver)
	if !ok {
		return nil, errors.New("unsupported database driver")
	}
	if err := driver.validate(c); err != nil {
		return nil, err
	}
	open := driver.open
	if existing {
		open = driver.openExisting
	}
	db, err := open(c)
	if err != nil {
		return nil, err
	}
	opened := false
	defer func() {
		if !opened {
			_ = db.Close()
		}
	}()
	if err = db.PingContext(ctx); err != nil {
		return nil, errors.New("database connection failed; verify configuration and availability")
	}
	files := driver.migrations
	if files == nil {
		files, err = fs.Sub(migrationFiles, driver.migrationDirectory)
	}
	if err != nil {
		return nil, errors.New("embedded migrations unavailable")
	}
	versionTable := driver.versionTable
	if versionTable == "" {
		versionTable = "goose_db_version"
	}
	if existing {
		var version int64
		if err := db.QueryRowContext(ctx, "SELECT version_id FROM "+versionTable+" ORDER BY id DESC LIMIT 1").Scan(&version); err != nil {
			return nil, errors.New("existing database schema required; check the service configuration")
		}
	}
	p, err := goose.NewProvider(driver.dialect, db, files, goose.WithTableName(versionTable))
	if err != nil {
		return nil, errors.New("cannot initialize database migrations")
	}
	// Reject a database newer than this binary instead of accidentally downgrading.
	version, err := p.GetDBVersion(ctx)
	if err != nil {
		return nil, errors.New("cannot read database migration version")
	}
	sources := p.ListSources()
	if len(sources) == 0 {
		return nil, errors.New("database migrations unavailable")
	}
	expected := sources[len(sources)-1].Version
	if version > expected {
		return nil, errors.New("database schema is newer than this server; use a compatible binary")
	}
	if existing && version != expected {
		return nil, errors.New("database schema does not match; use the same binary version as the service")
	}
	if !existing {
		if _, err = p.Up(ctx); err != nil {
			return nil, errors.New("database migration failed; service will not start")
		}
	}
	s := &Store{db: db, provider: p, expected: expected, storage: driver.capability, driver: driver}
	if err = s.Ready(ctx); err != nil {
		return nil, err
	}
	opened = true
	return s, nil
}

func (s *Store) Ready(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return errors.New("database unavailable")
	}
	v, err := s.provider.GetDBVersion(ctx)
	if err != nil || v != s.expected {
		return errors.New("database schema is not ready")
	}
	return nil
}
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) StorageCapability() capability.Storage { return s.storage }
