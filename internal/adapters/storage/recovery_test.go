package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/alibaba/open-guardrail/internal/config"
)

func TestOpenExisting(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "missing")
	c := config.Config{DataDir: dir, Database: config.Database{Driver: "sqlite"}}
	if s, err := OpenExisting(ctx, c); err == nil {
		s.Close()
		t.Fatal("opened missing database")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("recovery created a data directory")
	}
	s, err := Open(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = OpenExisting(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("DROP TABLE goose_db_version"); err != nil {
		t.Fatal(err)
	}
	if err = s.Ready(ctx); err != nil {
		t.Fatalf("readiness requires migration metadata: %v", err)
	}
	s.Close()
	s, err = OpenExisting(ctx, c)
	if err != nil {
		t.Fatalf("existing schema requires migration metadata: %v", err)
	}
	defer s.Close()
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM sqlite_schema WHERE name='goose_db_version'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("external mode recreated migration metadata: %d %v", count, err)
	}
}

func TestExternalMigrationsDoNotCreateSchema(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "missing")
	c := config.Config{DataDir: dir, Database: config.Database{Driver: "sqlite", Migrations: "external"}}
	if s, err := Open(ctx, c); err == nil {
		s.Close()
		t.Fatal("external migration mode opened missing schema")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("external migration mode created database files")
	}
	c.Database.Migrations = "auto"
	s, err := Open(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("DELETE FROM goose_db_version"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	c.Database.Migrations = "external"
	s, err = Open(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM goose_db_version").Scan(&count); err != nil || count != 0 {
		t.Fatalf("external mode initialized migration metadata: %d %v", count, err)
	}
	s.Close()
}
