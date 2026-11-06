package ctlcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
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

func TestIdentityCLI(t *testing.T) {
	t.Run("local without token", func(t *testing.T) { testIdentityCLI(t, true) })
	t.Run("remote with token", func(t *testing.T) { testIdentityCLI(t, false) })
}

func TestLocalBootstrapNegotiatesCompatibilityOnce(t *testing.T) {
	var capabilitiesRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1alpha1/meta/capabilities":
			capabilitiesRequests++
			_, _ = w.Write([]byte(`{"apiVersions":["v1alpha1"],"cli":{"minimumVersion":"0.1.0-alpha.0","maximumVersionExclusive":"0.2.0"},"features":[],"storage":{"supportLevel":"official-local","multiInstance":false}}`))
		case "/api/v1alpha1/bootstrap":
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(`{"initialized":false,"requiresToken":false}`))
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"user":{"id":"user-1"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	passwordInput, err := os.CreateTemp(dir, "stdin-")
	if err != nil {
		t.Fatal(err)
	}
	defer passwordInput.Close()
	if _, err = passwordInput.WriteString("synthetic-password-2026\n"); err != nil {
		t.Fatal(err)
	}
	if _, err = passwordInput.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	originalStdin := os.Stdin
	os.Stdin = passwordInput
	defer func() { os.Stdin = originalStdin }()
	var out bytes.Buffer
	if err = run([]string{"bootstrap", "init", "--server", server.URL, "--username", "admin", "--password-stdin"}, &out); err != nil {
		t.Fatal(err)
	}
	if capabilitiesRequests != 1 {
		t.Fatalf("got %d capabilities requests, want 1", capabilitiesRequests)
	}
}

func TestBootstrapTokenErrorsHaveStableExitCodes(t *testing.T) {
	invalidToken := filepath.Join(t.TempDir(), "bootstrap.token")
	if err := os.WriteFile(invalidToken, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := bootstrapToken(identityOptions{tokenFile: invalidToken}); commandExitCode(err) != exitUsage {
		t.Fatalf("invalid token exit code = %d, want %d", commandExitCode(err), exitUsage)
	}

	for _, tc := range []struct {
		name        string
		initialized bool
		requires    bool
		want        int
	}{
		{name: "already initialized", initialized: true, want: exitServerRejected},
		{name: "remote requires token", requires: true, want: exitUsage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/v1alpha1/meta/capabilities":
					_, _ = w.Write([]byte(`{"apiVersions":["v1alpha1"],"cli":{"minimumVersion":"0.1.0-alpha.0","maximumVersionExclusive":"0.2.0"},"features":[],"storage":{"supportLevel":"official-local","multiInstance":false}}`))
				case "/api/v1alpha1/bootstrap":
					_, _ = fmt.Fprintf(w, `{"initialized":%t,"requiresToken":%t}`, tc.initialized, tc.requires)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			_, _, err := bootstrapToken(identityOptions{server: server.URL})
			if got := commandExitCode(err); got != tc.want {
				t.Fatalf("exit code = %d, want %d: %v", got, tc.want, err)
			}
		})
	}
}
func testIdentityCLI(t *testing.T, local bool) {
	dir := t.TempDir()
	store, err := storage.Open(context.Background(), config.Config{DataDir: dir, Database: config.Database{Driver: "sqlite"}})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := identity.New(store.Identity(), security.Passwords{})
	token := identity.RandomToken("ogb_")
	if err = svc.PrepareBootstrap(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(nil)
	origin := "http://" + server.Listener.Addr().String()
	server.Config.Handler = httpapi.New(store, httpapi.Options{Identity: svc, Origin: origin, LocalBootstrap: local})
	server.Start()
	defer server.Close()
	tokenPath := filepath.Join(dir, "bootstrap.token")
	resultPath := filepath.Join(dir, "machine.json")
	sessionPath := filepath.Join(dir, "session.json")
	t.Setenv("OPEN_GUARDRAIL_SESSION_FILE", sessionPath)
	if err = os.WriteFile(tokenPath, []byte(token), 0600); err != nil {
		t.Fatal(err)
	}
	stdin := func() {
		t.Helper()
		f, err := os.CreateTemp(dir, "stdin-")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.WriteString("synthetic-password-2026\n"); err != nil {
			t.Fatal(err)
		}
		if _, err = f.Seek(0, 0); err != nil {
			t.Fatal(err)
		}
		old := os.Stdin
		os.Stdin = f
		t.Cleanup(func() { os.Stdin = old; f.Close() })
	}
	var out bytes.Buffer
	if err = run([]string{"bootstrap", "init", "--server", origin, "--username", "admin", "--token-file", tokenPath, "--result-file", resultPath}, &out); err == nil || !strings.Contains(err.Error(), "flag provided but not defined: -result-file") {
		t.Fatal("bootstrap accepted an unsupported output flag")
	}
	if initialized, err := svc.Status(context.Background()); err != nil || initialized {
		t.Fatal("legacy command changed initialization state")
	}
	stdin()
	args := []string{"bootstrap", "init", "--server", origin, "--username", "admin", "--password-stdin"}
	if !local {
		args = append(args, "--token-file", tokenPath)
	}
	if err = run(args, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "ogk_") || strings.Contains(out.String(), "synthetic-password") {
		t.Fatal("CLI printed secret")
	}
	if _, err := os.Stat(resultPath); !os.IsNotExist(err) {
		t.Fatal("bootstrap created key file")
	}
	stdin()
	out.Reset()
	if err = run([]string{"auth", "login", "--server", origin, "--username", "admin", "--password-stdin"}, &out); err != nil {
		t.Fatal(err)
	}
	if err = run([]string{"auth", "create-key", "--server", origin, "--result-file", resultPath}, &out); err != nil {
		t.Fatal(err)
	}
	if data, err := security.ReadPrivateFile(resultPath); err != nil || !bytes.Contains(data, []byte("ogk_")) {
		t.Fatal("missing private key output")
	}
	if err = run([]string{"auth", "create-key", "--server", origin, "--session-file", sessionPath, "--result-file", resultPath}, &out); err == nil {
		t.Fatal("key output overwritten")
	}
	if strings.Contains(out.String(), "ogs_") || strings.Contains(out.String(), "ogk_") {
		t.Fatal("CLI printed session")
	}
	if err = run([]string{"auth", "whoami", "--server", origin}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "\"role\":\"ADMIN\"") {
		t.Fatal("wrong principal")
	}
	if err = run([]string{"auth", "whoami", "--server", "https://other.example"}, &out); err == nil {
		t.Fatal("credential forwarded to another server")
	}
	if err = run([]string{"auth", "logout", "--server", origin}, &out); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Fatal("session file retained")
	}
}
func TestIdentityCommandFlags(t *testing.T) {
	all := []string{"server", "username", "password-stdin", "token-file", "session-file", "result-file", "role", "scope", "expires-in"}
	cases := []struct {
		command string
		allowed string
	}{
		{"bootstrap init", "server username password-stdin token-file"},
		{"auth login", "server username password-stdin session-file"},
		{"auth logout", "server session-file"},
		{"auth whoami", "server session-file"},
		{"auth create-key", "server session-file result-file role scope expires-in"},
	}
	for _, tc := range cases {
		t.Run(tc.command, func(t *testing.T) {
			for _, helpFlag := range []string{"-h", "--help"} {
				var help bytes.Buffer
				if _, err := parseIdentityOptions(tc.command, []string{helpFlag}, &help); !errors.Is(err, flag.ErrHelp) {
					t.Fatalf("%s failed: %v", helpFlag, err)
				}
				for _, name := range all {
					allowed := strings.Contains(" "+tc.allowed+" ", " "+name+" ")
					visible := strings.Contains(help.String(), "  -"+name+" ") || strings.Contains(help.String(), "  -"+name+"\n")
					if visible != allowed {
						t.Errorf("incorrect help entry for %s", name)
					}
				}
			}
			for _, name := range all {
				allowed := strings.Contains(" "+tc.allowed+" ", " "+name+" ")
				value := "example"
				if name == "server" {
					value = "https://guardrail.example"
				} else if name == "password-stdin" {
					value = "true"
				} else if name == "role" {
					value = "POLICY_PUBLISHER"
				} else if name == "scope" {
					value = "runs:create-with-revision"
				} else if name == "expires-in" {
					value = "1h"
				}
				var out bytes.Buffer
				_, err := parseIdentityOptions(tc.command, []string{"--" + name + "=" + value}, &out)
				if allowed && err != nil {
					t.Errorf("supported flag %s rejected: %v", name, err)
				} else if !allowed && (err == nil || !strings.Contains(err.Error(), "flag provided but not defined")) {
					t.Errorf("unsupported flag %s was not rejected: %v", name, err)
				}
			}
		})
	}
	if _, err := parseIdentityOptions("auth create-key", []string{"--role=ADMIN"}, &bytes.Buffer{}); err == nil {
		t.Fatal("administrator role accepted for machine key")
	}
	if _, err := parseIdentityOptions("auth create-key", []string{"--role=POLICY_PUBLISHER", "--scope=runs:create-with-revision"}, &bytes.Buffer{}); err == nil {
		t.Fatal("policy publisher accepted Run scope")
	}
	if _, err := parseIdentityOptions("auth create-key", []string{"--scope=unknown"}, &bytes.Buffer{}); err == nil {
		t.Fatal("unknown machine scope accepted")
	}
}

func TestIdentityGroupHelp(t *testing.T) {
	cases := []struct {
		group   string
		command string
	}{
		{"bootstrap", "init"},
		{"auth", "create-key"},
	}
	for _, tc := range cases {
		for _, helpFlag := range []string{"-h", "--help"} {
			t.Run(tc.group+" "+helpFlag, func(t *testing.T) {
				var out bytes.Buffer
				if err := run([]string{tc.group, helpFlag}, &out); err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(out.String(), "Usage: guardrailctl "+tc.group) || !strings.Contains(out.String(), tc.command) {
					t.Fatalf("unexpected help: %s", out.String())
				}
			})
		}
	}
}

func TestDefaultSessionFile(t *testing.T) {
	t.Run("environment override", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "session.json")
		t.Setenv("OPEN_GUARDRAIL_SESSION_FILE", path)
		got, managedDirectory, err := defaultSessionFile()
		if err != nil || got != path || managedDirectory != "" {
			t.Fatalf("path=%q managed=%q err=%v", got, managedDirectory, err)
		}
		opts, err := parseIdentityOptions("auth whoami", nil, &bytes.Buffer{})
		if err != nil || opts.sessionFile != path || !opts.defaultSession {
			t.Fatalf("options=%+v err=%v", opts, err)
		}
	})
	t.Run("operating system config directory", func(t *testing.T) {
		t.Setenv("OPEN_GUARDRAIL_SESSION_FILE", "")
		configDirectory, err := os.UserConfigDir()
		if err != nil {
			t.Fatal(err)
		}
		got, managedDirectory, err := defaultSessionFile()
		wantDirectory := filepath.Join(configDirectory, "open-guardrail")
		if err != nil || got != filepath.Join(wantDirectory, "session.json") || managedDirectory != wantDirectory {
			t.Fatalf("path=%q managed=%q err=%v", got, managedDirectory, err)
		}
	})
}

func TestDefaultSessionLifecycle(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "open-guardrail")
	if err := ensurePrivateSessionDirectory(directory); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(directory)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("directory mode=%v err=%v", info, err)
	}
	path := filepath.Join(directory, "session.json")
	if err = removeExpiredSessionFile(path, "https://guardrail.example", 100); err != nil {
		t.Fatal("missing session should be accepted", err)
	}
	data, _ := json.Marshal(credentialFile{Server: "https://guardrail.example", Token: "ogs_synthetic", ExpiresAt: 100})
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = removeExpiredSessionFile(path, "https://guardrail.example", 99); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path); err != nil {
		t.Fatal("live session was removed", err)
	}
	if err = removeExpiredSessionFile(path, "https://guardrail.example", 100); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("expired session was retained", err)
	}

	if err = os.WriteFile(path, []byte("not-json"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = removeExpiredSessionFile(path, "https://guardrail.example", 100); err == nil ||
		!strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("invalid session error = %v", err)
	}
	if _, err = os.Stat(path); err != nil {
		t.Fatal("invalid session was silently removed", err)
	}

	data, _ = json.Marshal(credentialFile{Server: "https://old.example", Token: "ogs_synthetic", ExpiresAt: 200})
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = removeExpiredSessionFile(path, "https://guardrail.example", 100); err == nil ||
		!strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "https://old.example") ||
		!strings.Contains(err.Error(), "auth logout") {
		t.Fatalf("other-server session error = %v", err)
	}
	if _, err = os.Stat(path); err != nil {
		t.Fatal("live other-server session was silently removed", err)
	}

	if err = removeExpiredSessionFile(path, "https://guardrail.example", 200); err != nil {
		t.Fatal("expired other-server session should be removed", err)
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("expired other-server session was retained", err)
	}
}

func TestGeneratedClientNoRedirect(t *testing.T) {
	reached := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	defer target.Close()
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer gateway.Close()
	client, err := newAPIClient(gateway.URL, "synthetic-secret", time.Second, identityResponseLimit, true)
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.generated.GetCurrentAdministratorWithResponse(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err = validateResponse(result, identityResponseLimit, "identity request", false); err == nil {
		t.Fatal("redirect accepted")
	}
	if reached {
		t.Fatal("credential followed redirect")
	}
}

func TestSaveNewCredential(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credential.json")
	want := credentialFile{Server: "https://guardrail.example", Token: "synthetic-token", ExpiresAt: 123}
	err := saveNewCredential(path, func() (credentialFile, error) {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal("output was not reserved before issuing credentials:", err)
		}
		if info.Mode().Perm() != 0600 || info.Size() != 0 {
			t.Fatal("output must be empty and private before issuing credentials")
		}
		return want, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := security.ReadPrivateFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got credentialFile
	if err := json.Unmarshal(data, &got); err != nil || got != want {
		t.Fatalf("stored credential: %+v, error: %v", got, err)
	}
	err = saveNewCredential(path, func() (credentialFile, error) {
		t.Fatal("existing output must prevent credential issuance")
		return credentialFile{}, nil
	})
	if err == nil {
		t.Fatal("existing output accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, after) {
		t.Fatal("existing credential changed")
	}
}

func TestSaveNewCredentialCleansUpAfterFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credential.json")
	wantErr := errors.New("issuance failed")
	err := saveNewCredential(path, func() (credentialFile, error) {
		return credentialFile{}, wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected issuance error, got %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("failed issuance left output behind: %v", err)
	}
}
