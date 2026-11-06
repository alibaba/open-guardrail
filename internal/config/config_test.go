package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIdentityOriginConfiguration(t *testing.T) {
	cleanEnv(t)
	t.Setenv("OPEN_GUARDRAIL_PUBLIC_URL", "")
	if _, err := Load("", "0.0.0.0:8080", ""); err == nil {
		t.Fatal("public plaintext deployment accepted")
	}
	for _, origin := range []string{"http://example.com", "https://user:secret@example.com", "https://example.com/path", "https://example.com?key=value"} {
		t.Setenv("OPEN_GUARDRAIL_PUBLIC_URL", origin)
		if _, err := Load("", "0.0.0.0:8080", ""); err == nil {
			t.Fatal("unsafe public origin accepted")
		}
	}
	t.Setenv("OPEN_GUARDRAIL_PUBLIC_URL", "https://guardrail.example")
	if _, err := Load("", "0.0.0.0:8080", ""); err != nil {
		t.Fatal(err)
	}
}

func cleanEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"OPEN_GUARDRAIL_PUBLIC_URL", "OPEN_GUARDRAIL_LISTEN", "OPEN_GUARDRAIL_DATA_DIR", "OPEN_GUARDRAIL_DATABASE_DRIVER", "OPEN_GUARDRAIL_DATABASE_MIGRATIONS", "DATABASE_URL"} {
		old, ok := os.LookupEnv(key)
		_ = os.Unsetenv(key)
		t.Cleanup(func() {
			if ok {
				_ = os.Setenv(key, old)
			} else {
				_ = os.Unsetenv(key)
			}
		})
	}
}
func TestPrecedence(t *testing.T) {
	cleanEnv(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("listen: 127.0.0.1:8001\ndata_dir: yaml-data\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPEN_GUARDRAIL_LISTEN", "127.0.0.1:8002")
	c, err := Load(path, "127.0.0.1:8003", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != "127.0.0.1:8003" || c.Database.Driver != "sqlite" {
		t.Fatalf("unexpected config: %+v", c)
	}
}
func TestRejectInvalidConfiguration(t *testing.T) {
	for _, source := range []string{"unknown: true\n", "database:\n  driver: INVALID/NAME\n", "database:\n  migrations: unsupported\n", "---\nlisten: 127.0.0.1:8000\n---\nlisten: 127.0.0.1:9000\n"} {
		t.Run(source, func(t *testing.T) {
			cleanEnv(t)
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path, "", ""); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestExternalMigrationsConfiguration(t *testing.T) {
	cleanEnv(t)
	t.Setenv("OPEN_GUARDRAIL_DATABASE_MIGRATIONS", "external")
	c, err := Load("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Database.Migrations != "external" {
		t.Fatalf("unexpected migration mode: %q", c.Database.Migrations)
	}
}
func TestURLDoesNotSelectBackend(t *testing.T) {
	cleanEnv(t)
	t.Setenv("DATABASE_URL", "postgres://example.invalid/guardrail")
	c, err := Load("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Database.Driver != "sqlite" {
		t.Fatal("DATABASE_URL selected a backend")
	}
}

func TestDriverSupportIsDeferredToStorageRegistry(t *testing.T) {
	cleanEnv(t)
	t.Setenv("OPEN_GUARDRAIL_DATABASE_DRIVER", "future-driver")
	c, err := Load("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Database.Driver != "future-driver" {
		t.Fatalf("unexpected driver: %q", c.Database.Driver)
	}
}

func TestConfigurationSizeLimit(t *testing.T) {
	prefix := "listen: 127.0.0.1:8000\n#"
	atLimit := prefix + strings.Repeat("x", maxConfigBytes-len(prefix)-1) + "\n"
	for _, tc := range []struct {
		name    string
		source  string
		wantErr bool
	}{
		{"below limit", atLimit[:len(atLimit)-1], false},
		{"at limit", atLimit, false},
		{"over limit", atLimit + "\n", true},
		{"second document beyond limit", atLimit + "---\nlisten: 127.0.0.1:9000\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cleanEnv(t)
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tc.source), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(path, "", "")
			if tc.wantErr {
				if err == nil || err.Error() != "configuration file exceeds 1 MiB limit" {
					t.Fatalf("expected size limit error, got %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}
