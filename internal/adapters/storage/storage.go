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
	db      *sql.DB
	storage capability.Storage
	driver  driverDefinition
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

// OpenExisting uses an externally maintained schema without migration metadata.
// It prepares the default workspace/project just like Open, but never runs DDL.
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
	if !existing {
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
		p, err := goose.NewProvider(driver.dialect, db, files, goose.WithTableName(versionTable))
		if err != nil {
			return nil, errors.New("cannot initialize database migrations")
		}
		version, err := p.GetDBVersion(ctx)
		if err != nil {
			return nil, errors.New("cannot read database migration version")
		}
		sources := p.ListSources()
		if len(sources) == 0 {
			return nil, errors.New("database migrations unavailable")
		}
		if version > sources[len(sources)-1].Version {
			return nil, errors.New("database schema is newer than this server; use a compatible binary")
		}
		if _, err = p.Up(ctx); err != nil {
			return nil, errors.New("database migration failed; service will not start")
		}
	}
	s := &Store{db: db, storage: driver.capability, driver: driver}
	if err = s.ensureDefaultProject(ctx); err != nil {
		return nil, errors.New("cannot prepare default workspace/project; verify database schema, data and write permissions")
	}
	opened = true
	return s, nil
}

func (s *Store) Ready(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return errors.New("database unavailable")
	}
	return nil
}
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) StorageCapability() capability.Storage { return s.storage }
