package ctlcmd

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	contract "github.com/alibaba/open-guardrail/api/openapi"
	"github.com/alibaba/open-guardrail/internal/adapters/security"
	"github.com/alibaba/open-guardrail/internal/application/identity"
)

const identityResponseLimit = 16 * 1024

type credentialFile struct {
	Server    string `json:"server"`
	Token     string `json:"token"`
	RunID     string `json:"runId,omitempty"`
	KeyID     string `json:"keyId,omitempty"`
	ExpiresAt int64  `json:"expiresAt,omitempty"`
}

var errCredentialOutputExists = errors.New("credential output already exists")

type identityOptions struct {
	server, username, tokenFile, sessionFile, resultFile, role, scope string
	passwordStdin                                                     bool
	defaultSession                                                    bool
	managedSessionDirectory                                           string
	keyID, after                                                      string
	expectedVersion                                                   int64
	pageSize                                                          int
	lifetime                                                          time.Duration
	lifetimeSet                                                       bool
}

func runIdentity(args []string, out io.Writer) error {
	if len(args) == 2 && (args[1] == "-h" || args[1] == "--help") {
		printIdentityGroupHelp(args[0], out)
		return nil
	}
	if len(args) < 2 {
		return usageError("usage: guardrailctl bootstrap init|auth login|logout|whoami|create-key|list-keys|rotate-key|revoke-key [flags]")
	}
	command := args[0] + " " + args[1]
	switch command {
	case "bootstrap init", "auth login", "auth logout", "auth whoami", "auth create-key", "auth list-keys", "auth rotate-key", "auth revoke-key":
	default:
		return usageError("unsupported identity command")
	}
	opts, err := parseIdentityOptions(command, args[2:], out)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return usageError(err.Error())
	}
	switch command {
	case "bootstrap init":
		return initializeAdministrator(opts, out)
	case "auth login":
		return loginAdministrator(opts, out)
	default:
		session, err := readSession(opts)
		if err != nil {
			return err
		}
		switch command {
		case "auth create-key":
			return createMachineKey(opts, session.Token, out)
		case "auth list-keys":
			return listMachineKeys(opts, session.Token, out)
		case "auth rotate-key":
			return rotateMachineKey(opts, session.Token, out)
		case "auth revoke-key":
			return revokeMachineKey(opts, session.Token, out)
		case "auth logout":
			return logoutAdministrator(opts, session.Token, out)
		default:
			return showAdministrator(opts, session.Token, out)
		}
	}
}

func printIdentityGroupHelp(group string, out io.Writer) {
	switch group {
	case "bootstrap":
		fmt.Fprintln(out, "Usage: guardrailctl bootstrap init [flags]")
		fmt.Fprintln(out, "\nCommands:\n  init    Create the first local administrator")
	case "auth":
		fmt.Fprintln(out, "Usage: guardrailctl auth <command> [flags]")
		fmt.Fprintln(out, "\nCommands:\n  login       Sign in and save a CLI session\n  logout      Revoke the current CLI session\n  whoami      Show the current administrator\n  create-key  Create a scoped machine key\n  list-keys   List key metadata without secrets\n  rotate-key  Replace an active key secret\n  revoke-key  Revoke a machine key")
	}
}

func parseIdentityOptions(command string, args []string, out io.Writer) (identityOptions, error) {
	var opts identityOptions
	var sessionDefault, managedSessionDirectory string
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(out)
	flags.StringVar(&opts.server, "server", defaultServerURL(), "service origin (HTTPS or literal loopback HTTP)")
	if command == "bootstrap init" || command == "auth login" {
		flags.StringVar(&opts.username, "username", "", "administrator username")
		flags.BoolVar(&opts.passwordStdin, "password-stdin", false, "read password from stdin instead of a hidden prompt")
	}
	switch command {
	case "bootstrap init":
		flags.StringVar(&opts.tokenFile, "token-file", "", "private bootstrap token file (required only for remote setup)")
	case "auth login", "auth logout", "auth whoami", "auth create-key", "auth list-keys", "auth rotate-key", "auth revoke-key":
		var err error
		sessionDefault, managedSessionDirectory, err = defaultSessionFile()
		if err != nil {
			return opts, err
		}
		flags.StringVar(&opts.sessionFile, "session-file", sessionDefault, "private CLI session file")
	}
	if command == "auth create-key" {
		flags.StringVar(&opts.resultFile, "result-file", "", "new private file for the machine key")
		flags.StringVar(&opts.role, "role", "RUN_ORCHESTRATOR", "machine role: RUN_ORCHESTRATOR or POLICY_PUBLISHER")
		flags.StringVar(&opts.scope, "scope", "", "optional RUN_ORCHESTRATOR scope: runs:create-with-revision")
	}
	if command == "auth create-key" || command == "auth rotate-key" {
		fallback := time.Duration(0)
		if command == "auth create-key" {
			fallback = time.Duration(identity.DefaultAPIKeyLifetimeSeconds) * time.Second
		}
		flags.DurationVar(&opts.lifetime, "expires-in", fallback, "key lifetime from 1m to 8760h; rotation preserves expiry when omitted")
	}
	if command == "auth rotate-key" {
		flags.StringVar(&opts.resultFile, "result-file", "", "new private file for the replacement key")
		flags.Int64Var(&opts.expectedVersion, "expected-version", 0, "current version from auth list-keys (required)")
	}
	if command == "auth rotate-key" || command == "auth revoke-key" {
		flags.StringVar(&opts.keyID, "key-id", "", "key ID from auth list-keys (required)")
	}
	if command == "auth list-keys" {
		flags.IntVar(&opts.pageSize, "page-size", 20, "number of keys, from 1 to 100")
		flags.StringVar(&opts.after, "after", "", "nextAfter cursor from a previous page")
	}
	if err := flags.Parse(args); err != nil {
		return opts, err
	}
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "expires-in" {
			opts.lifetimeSet = true
		}
	})
	if opts.lifetimeSet && (opts.lifetime < time.Minute || opts.lifetime > time.Duration(identity.MaxAPIKeyLifetimeSeconds)*time.Second || opts.lifetime%time.Second != 0) {
		return opts, usageError("--expires-in must be whole seconds from 1m to 8760h")
	}
	if (command == "auth rotate-key" || command == "auth revoke-key") && !identity.ValidAPIKeyID(opts.keyID) {
		return opts, usageError("--key-id must be a key ID from auth list-keys")
	}
	if command == "auth rotate-key" && opts.expectedVersion < 1 {
		return opts, usageError("--expected-version must be the current positive key version")
	}
	if command == "auth list-keys" && (opts.pageSize < 1 || opts.pageSize > 100 || (opts.after != "" && !identity.ValidAPIKeyID(opts.after))) {
		return opts, usageError("invalid --page-size or --after cursor")
	}
	if strings.HasPrefix(command, "auth ") {
		if opts.sessionFile == "" {
			return opts, usageError("--session-file cannot be empty")
		}
		opts.defaultSession = opts.sessionFile == sessionDefault
		if opts.defaultSession {
			opts.managedSessionDirectory = managedSessionDirectory
		}
	}
	if flags.NArg() != 0 {
		return opts, usageError("unexpected positional arguments")
	}
	if command == "auth create-key" && opts.role != "RUN_ORCHESTRATOR" && opts.role != "POLICY_PUBLISHER" {
		return opts, usageError("--role must be RUN_ORCHESTRATOR or POLICY_PUBLISHER")
	}
	if command == "auth create-key" && opts.scope != "" &&
		(opts.role != "RUN_ORCHESTRATOR" || opts.scope != "runs:create-with-revision") {
		return opts, usageError("--scope supports only runs:create-with-revision with RUN_ORCHESTRATOR")
	}
	u, err := url.Parse(opts.server)
	if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return opts, usageError("server must be an origin without credentials, path, query or fragment")
	}
	loopback := net.ParseIP(u.Hostname())
	if u.Scheme != "https" && (u.Scheme != "http" || loopback == nil || !loopback.IsLoopback()) {
		return opts, usageError("credentials require HTTPS except for literal loopback addresses")
	}
	opts.server = u.String()
	return opts, nil
}

func initializeAdministrator(opts identityOptions, out io.Writer) error {
	if opts.username == "" {
		return usageError("--username is required")
	}
	token, client, err := bootstrapToken(opts)
	if err != nil {
		return err
	}
	if client == nil {
		client, err = newIdentityClient(opts.server, token)
		if err != nil {
			return err
		}
	}
	password, err := security.ReadPassword(opts.passwordStdin)
	if err != nil {
		return err
	}
	params := &contract.BootstrapAdministratorParams{XGuardrailCSRF: contract.BootstrapAdministratorParamsXGuardrailCSRFN1}
	result, err := client.generated.BootstrapAdministratorWithResponse(context.Background(), params,
		contract.BootstrapRequest{Username: opts.username, Password: &password})
	if err != nil {
		return transportError(err, true)
	}
	if err = validateResponse(result, identityResponseLimit, "bootstrap request", true); err != nil {
		return err
	}
	if result.JSON201 == nil || result.JSON201.User.Id == "" {
		return internalError("invalid bootstrap response; check initialization status before retrying")
	}
	_, err = fmt.Fprintln(out, "Administrator initialized. Log in through the browser or CLI; create a machine key only when needed.")
	return err
}

func bootstrapToken(opts identityOptions) (string, *apiClient, error) {
	if opts.tokenFile != "" {
		data, err := security.ReadPrivateFile(opts.tokenFile)
		if err != nil {
			return "", nil, err
		}
		token := strings.TrimSpace(string(data))
		if !strings.HasPrefix(token, "ogb_") {
			return "", nil, usageError("invalid bootstrap token file")
		}
		return token, nil, nil
	}
	client, err := newIdentityClient(opts.server, "")
	if err != nil {
		return "", nil, err
	}
	status, err := client.generated.GetBootstrapStatusWithResponse(context.Background())
	if err != nil {
		return "", nil, transportError(err, false)
	}
	if err = validateResponse(status, identityResponseLimit, "bootstrap status request", false); err != nil {
		return "", nil, err
	}
	if status.JSON200 == nil {
		return "", nil, internalError("cannot determine setup mode; provide --token-file")
	}
	if status.JSON200.Initialized {
		return "", nil, &commandError{kind: exitServerRejected, message: "already initialized; log in instead"}
	}
	if status.JSON200.RequiresToken {
		return "", nil, usageError("remote setup requires --token-file")
	}
	return "", client, nil
}

func loginAdministrator(opts identityOptions, out io.Writer) error {
	if opts.username == "" {
		return usageError("--username is required")
	}
	if opts.managedSessionDirectory != "" {
		if err := ensurePrivateSessionDirectory(opts.managedSessionDirectory); err != nil {
			return err
		}
	}
	if opts.defaultSession {
		if err := removeExpiredSessionFile(opts.sessionFile, opts.server, time.Now().Unix()); err != nil {
			return err
		}
	}
	client, err := newIdentityClient(opts.server, "")
	if err != nil {
		return err
	}
	err = saveNewCredential(opts.sessionFile, func() (credentialFile, error) {
		password, err := security.ReadPassword(opts.passwordStdin)
		if err != nil {
			return credentialFile{}, err
		}
		params := &contract.LoginAdministratorParams{XGuardrailCSRF: contract.LoginAdministratorParamsXGuardrailCSRFN1}
		kind := contract.Cli
		result, err := client.generated.LoginAdministratorWithResponse(context.Background(), params,
			contract.LoginRequest{Username: opts.username, Password: &password, Kind: &kind})
		if err != nil {
			return credentialFile{}, transportError(err, true)
		}
		if err = validateResponse(result, identityResponseLimit, "login request", true); err != nil {
			return credentialFile{}, err
		}
		if result.JSON201 == nil || result.JSON201.Token == nil || *result.JSON201.Token == "" {
			return credentialFile{}, internalError("invalid login response")
		}
		return credentialFile{Server: opts.server, Token: *result.JSON201.Token, ExpiresAt: result.JSON201.ExpiresAt}, nil
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "Success. Administrator session saved to %q; no secret was printed.\n", opts.sessionFile)
	return err
}

func defaultSessionFile() (string, string, error) {
	if configured := os.Getenv("OPEN_GUARDRAIL_SESSION_FILE"); configured != "" {
		return configured, "", nil
	}
	configDirectory, err := os.UserConfigDir()
	if err != nil || configDirectory == "" {
		return "", "", usageError("cannot determine the default session location; use --session-file")
	}
	managedDirectory := filepath.Join(configDirectory, "open-guardrail")
	return filepath.Join(managedDirectory, "session.json"), managedDirectory, nil
}

func ensurePrivateSessionDirectory(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return errors.New("cannot create the default session directory")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("default session directory must be a real directory")
	}
	if info.Mode().Perm() != 0700 {
		if err = os.Chmod(path, 0700); err != nil {
			return errors.New("cannot secure the default session directory")
		}
	}
	return nil
}

func removeExpiredSessionFile(path, server string, now int64) error {
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return errors.New("cannot inspect existing default session file")
	}
	data, err := security.ReadPrivateFile(path)
	if err != nil {
		return err
	}
	var saved credentialFile
	if json.Unmarshal(data, &saved) != nil || saved.Server == "" || !strings.HasPrefix(saved.Token, "ogs_") || saved.ExpiresAt <= 0 {
		return fmt.Errorf("default session file %q is invalid; move or remove it before logging in", path)
	}
	if saved.ExpiresAt <= now {
		if err = os.Remove(path); err != nil {
			return errors.New("cannot remove expired default session file")
		}
		return nil
	}
	if saved.Server != server {
		return fmt.Errorf("default session file %q belongs to %q; run auth logout against that server or use --session-file", path, saved.Server)
	}
	return nil
}

func readSession(opts identityOptions) (credentialFile, error) {
	var saved credentialFile
	if opts.sessionFile == "" {
		return saved, usageError("--session-file is required")
	}
	data, err := security.ReadPrivateFile(opts.sessionFile)
	if err != nil {
		return saved, err
	}
	if json.Unmarshal(data, &saved) != nil || saved.Server != opts.server || !strings.HasPrefix(saved.Token, "ogs_") {
		return credentialFile{}, errors.New("session file does not match this server")
	}
	return saved, nil
}

func createMachineKey(opts identityOptions, token string, out io.Writer) error {
	if opts.resultFile == "" {
		return usageError("--result-file is required")
	}
	client, err := newIdentityClient(opts.server, token)
	if err != nil {
		return err
	}
	err = saveNewCredential(opts.resultFile, func() (credentialFile, error) {
		params := &contract.CreateProjectAPIKeyParams{XGuardrailCSRF: contract.CreateProjectAPIKeyParamsXGuardrailCSRFN1}
		role := contract.CreateAPIKeyRequestRole(opts.role)
		seconds := int64(opts.lifetime / time.Second)
		request := contract.CreateAPIKeyRequest{Role: &role, ExpiresInSeconds: &seconds}
		if opts.scope != "" {
			scopes := []contract.CreateAPIKeyRequestScopes{contract.CreateAPIKeyRequestScopesRunsCreateWithRevision}
			request.Scopes = &scopes
		}
		result, err := client.generated.CreateProjectAPIKeyWithResponse(context.Background(), params, request)
		if err != nil {
			return credentialFile{}, transportError(err, true)
		}
		if err = validateResponse(result, identityResponseLimit, "API key request", true); err != nil {
			return credentialFile{}, err
		}
		if result.JSON201 == nil || result.JSON201.ApiKey == "" {
			return credentialFile{}, internalError("invalid key response; do not automatically retry")
		}
		return credentialFile{Server: opts.server, Token: result.JSON201.ApiKey, KeyID: result.JSON201.Id, ExpiresAt: result.JSON201.ExpiresAt}, nil
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, "Machine key saved to the requested private file; no secret was printed.")
	return err
}

// Reserve a private output file before issuing credentials. Existing files must
// never be overwritten, and a failed request must not leave an empty file behind.
func saveNewCredential(path string, issue func() (credentialFile, error)) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return errCredentialOutputExists
		}
		return errors.New("cannot create credential output; use a new file path")
	}
	completed := false
	defer func() {
		_ = file.Close()
		if !completed {
			_ = os.Remove(path)
		}
	}()
	credential, err := issue()
	if err != nil {
		return err
	}
	if err = json.NewEncoder(file).Encode(credential); err != nil {
		return errors.New("credential issued but could not be saved; do not automatically retry")
	}
	if err = file.Sync(); err != nil {
		return errors.New("cannot sync credential output; do not automatically retry")
	}
	completed = true
	return nil
}

func logoutAdministrator(opts identityOptions, token string, out io.Writer) error {
	client, err := newIdentityClient(opts.server, token)
	if err != nil {
		return err
	}
	params := &contract.LogoutAdministratorParams{XGuardrailCSRF: contract.LogoutAdministratorParamsXGuardrailCSRFN1}
	result, err := client.generated.LogoutAdministratorWithResponse(context.Background(), params)
	if err != nil {
		return transportError(err, true)
	}
	if err = validateResponse(result, identityResponseLimit, "logout request", true); err != nil {
		return err
	}
	if err := os.Remove(opts.sessionFile); err != nil {
		return errors.New("session revoked; remove local credential file manually")
	}
	_, err = fmt.Fprintln(out, "Logged out.")
	return err
}

func showAdministrator(opts identityOptions, token string, out io.Writer) error {
	client, err := newIdentityClient(opts.server, token)
	if err != nil {
		return err
	}
	result, err := client.generated.GetCurrentAdministratorWithResponse(context.Background())
	if err != nil {
		return transportError(err, false)
	}
	if err = validateResponse(result, identityResponseLimit, "identity request", false); err != nil {
		return err
	}
	if result.JSON200 == nil {
		return internalError("invalid identity response")
	}
	_, err = out.Write(result.Body)
	return err
}

func newIdentityClient(server, token string) (*apiClient, error) {
	return newCompatibleClient(server, token, identityResponseLimit)
}
