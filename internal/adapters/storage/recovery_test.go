package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/alibaba/open-guardrail/internal/application/identity"
	"github.com/alibaba/open-guardrail/internal/config"
)

func TestResetLocalAdmin(t *testing.T) {
	for _, driver := range registeredDrivers() {
		t.Run(driver.name, func(t *testing.T) {
			ctx := context.Background()
			s := identityStore(t, driver.name)
			svc := service(s)
			if err := svc.ResetLocalAdmin(ctx, "admin", testPassword); !errors.Is(err, identity.ErrUnauthorized) {
				t.Fatalf("reset before initialization: %v", err)
			}
			initialize(t, svc)
			cli := login(t, svc, testPassword)
			browser, err := svc.Login(ctx, identity.LoginRequest{Username: "admin", Password: testPassword, Kind: "browser"})
			if err != nil {
				t.Fatal(err)
			}
			key, err := svc.CreateAPIKey(ctx, cli.Token, "cli", "RUN_ORCHESTRATOR")
			if err != nil {
				t.Fatal(err)
			}
			if err = svc.ResetLocalAdmin(ctx, "renamed", "short"); !errors.Is(err, identity.ErrInvalid) {
				t.Fatalf("invalid password accepted: %v", err)
			}
			if _, err = svc.Authenticate(ctx, cli.Token, "cli"); err != nil {
				t.Fatal("invalid reset changed the session", err)
			}
			if err = svc.ResetLocalAdmin(ctx, "renamed", "new-synthetic-password"); err != nil {
				t.Fatal(err)
			}
			for kind, token := range map[string]string{"cli": cli.Token, "browser": browser.Token} {
				if _, err = svc.Authenticate(ctx, token, kind); !errors.Is(err, identity.ErrUnauthorized) {
					t.Fatalf("%s session survived reset: %v", kind, err)
				}
			}
			if _, err = svc.Authenticate(ctx, key.APIKey, "key"); err != nil {
				t.Fatal("machine key invalidated", err)
			}
			if _, err = svc.Login(ctx, identity.LoginRequest{Username: "renamed", Password: testPassword, Kind: "cli"}); !errors.Is(err, identity.ErrUnauthorized) {
				t.Fatalf("old password accepted: %v", err)
			}
			if _, err = svc.Login(ctx, identity.LoginRequest{Username: "renamed", Password: "new-synthetic-password", Kind: "cli"}); err != nil {
				t.Fatal("new credentials failed", err)
			}
			if err = svc.ResetLocalAdmin(ctx, "", testPassword); err != nil {
				t.Fatal(err)
			}
			if _, err = svc.Login(ctx, identity.LoginRequest{Username: "renamed", Password: testPassword, Kind: "cli"}); err != nil {
				t.Fatal("empty username did not preserve name", err)
			}
		})
	}
}

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
	if _, err = s.db.Exec("INSERT INTO goose_db_version (version_id, is_applied) VALUES (999, true)"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if s, err = OpenExisting(ctx, c); err == nil {
		s.Close()
		t.Fatal("opened incompatible schema")
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
	s.Close()
	c.Database.Migrations = "external"
	s, err = Open(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
}
