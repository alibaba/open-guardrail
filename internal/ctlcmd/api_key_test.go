package ctlcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alibaba/open-guardrail/internal/adapters/httpapi"
	"github.com/alibaba/open-guardrail/internal/adapters/security"
	"github.com/alibaba/open-guardrail/internal/adapters/storage"
	"github.com/alibaba/open-guardrail/internal/application/identity"
	"github.com/alibaba/open-guardrail/internal/config"
)

func TestMachineKeyCLI(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, config.Config{DataDir: t.TempDir(), Database: config.Database{Driver: "sqlite"}})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := identity.New(store.Identity(), security.Passwords{})
	if _, err := svc.BootstrapLocal(ctx, identity.BootstrapRequest{Username: "admin", Password: "synthetic-password"}); err != nil {
		t.Fatal(err)
	}
	session, err := svc.Login(ctx, identity.LoginRequest{Username: "admin", Password: "synthetic-password", Kind: "cli"})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(nil)
	origin := "http://" + server.Listener.Addr().String()
	server.Config.Handler = httpapi.New(store, httpapi.Options{Identity: svc, Origin: origin})
	server.Start()
	defer server.Close()
	directory := t.TempDir()
	sessionPath := filepath.Join(directory, "session.json")
	if err := saveNewCredential(sessionPath, func() (credentialFile, error) {
		return credentialFile{Server: origin, Token: session.Token, ExpiresAt: session.ExpiresAt}, nil
	}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	invoke := func(command string, flags ...string) error {
		args := []string{"auth", command, "--server", origin, "--session-file", sessionPath}
		return run(append(args, flags...), &output)
	}
	read := func(path string) credentialFile {
		t.Helper()
		data, err := security.ReadPrivateFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var credential credentialFile
		if err := json.Unmarshal(data, &credential); err != nil {
			t.Fatal(err)
		}
		return credential
	}
	keyPath := filepath.Join(directory, "key.json")
	if err := invoke("create-key", "--expires-in", "1h", "--result-file", keyPath); err != nil {
		t.Fatal(err)
	}
	key := read(keyPath)
	if !identity.ValidAPIKeyID(key.KeyID) || key.ExpiresAt <= time.Now().Unix() {
		t.Fatal("credential file lacks lifecycle metadata")
	}
	if err := invoke("list-keys"); err != nil {
		t.Fatal(err)
	}
	if err := invoke("rotate-key", "--key-id", key.KeyID, "--expected-version", "1", "--result-file", keyPath); err == nil {
		t.Fatal("existing output permitted rotation")
	}
	if _, err := svc.Authenticate(ctx, key.Token, "key"); err != nil {
		t.Fatal("output failure invalidated the current secret")
	}
	replacementPath := filepath.Join(directory, "replacement.json")
	if err := invoke("rotate-key", "--key-id", key.KeyID, "--expected-version", "1", "--result-file", replacementPath); err != nil {
		t.Fatal(err)
	}
	replacement := read(replacementPath)
	if replacement.Token == key.Token || replacement.KeyID != key.KeyID || replacement.ExpiresAt != key.ExpiresAt {
		t.Fatal("incorrect replacement credential")
	}
	retryPath := filepath.Join(directory, "retry.json")
	if err := invoke("rotate-key", "--key-id", key.KeyID, "--expected-version", "1", "--result-file", retryPath); err == nil {
		t.Fatal("stale rotation accepted")
	}
	if _, err := os.Stat(retryPath); !os.IsNotExist(err) {
		t.Fatal("failed rotation retained an empty credential file")
	}
	for range 2 {
		if err := invoke("revoke-key", "--key-id", key.KeyID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.Authenticate(ctx, replacement.Token, "key"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("revoked CLI credential still authenticates")
	}
	if strings.Contains(output.String(), key.Token) || strings.Contains(output.String(), replacement.Token) || strings.Contains(output.String(), session.Token) {
		t.Fatal("CLI printed a secret")
	}
}

func TestMachineKeyCLIRejectsInvalidLifetimes(t *testing.T) {
	for _, lifetime := range []string{"0", "-1h", "59s", "60.5s", "8761h"} {
		if _, err := parseIdentityOptions("auth create-key", []string{"--expires-in", lifetime}, &bytes.Buffer{}); err == nil {
			t.Fatalf("accepted invalid lifetime %q", lifetime)
		}
	}
	if _, err := parseIdentityOptions("auth rotate-key", []string{"--key-id", "key_" + strings.Repeat("a", 32)}, &bytes.Buffer{}); err == nil {
		t.Fatal("rotation accepted without an expected version")
	}
}
