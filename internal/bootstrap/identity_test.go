package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/alibaba/open-guardrail/internal/adapters/storage"
	"github.com/alibaba/open-guardrail/internal/application/identity"
	"github.com/alibaba/open-guardrail/internal/config"
)

func TestBootstrapTokenLifecycle(t *testing.T) {
	ctx := context.Background()
	c := config.Config{DataDir: t.TempDir(), Database: config.Database{Driver: "sqlite"}}
	store, err := storage.Open(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc, err := prepareIdentity(ctx, c, store)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(c.DataDir, "bootstrap.token")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("insecure bootstrap file")
	}
	if _, err = prepareIdentity(ctx, c, store); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatal("restart changed unexpired token")
	}
	token := string(before[:len(before)-1])
	if _, err = svc.Bootstrap(ctx, token, identity.BootstrapRequest{Username: "admin", Password: "synthetic-password-2026"}); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err = prepareIdentity(ctx, c, store); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("initialized service regenerated a bootstrap token")
	}
}
