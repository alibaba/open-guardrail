package storage

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alibaba/open-guardrail/internal/adapters/security"
	"github.com/alibaba/open-guardrail/internal/application/identity"
)

const testPassword = "synthetic-password-2026"

func identityStore(t *testing.T, backend string) *Store {
	t.Helper()
	c := testConfig(t, backend)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := Open(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}
func TestIdentityBackends(t *testing.T) {
	for _, driver := range registeredDrivers() {
		t.Run(driver.name, func(t *testing.T) {
			t.Run("lifecycle", func(t *testing.T) { identityLifecycle(t, identityStore(t, driver.name)) })
			t.Run("bootstrap race", func(t *testing.T) { bootstrapRace(t, identityStore(t, driver.name)) })
			t.Run("rollback", func(t *testing.T) { bootstrapRollback(t, identityStore(t, driver.name)) })
			t.Run("expiry and login failures", func(t *testing.T) { identityExpiry(t, identityStore(t, driver.name)) })
			t.Run("key creation", func(t *testing.T) { keyCreation(t, identityStore(t, driver.name)) })
			t.Run("local setup with sign-in", func(t *testing.T) { localSetupSession(t, identityStore(t, driver.name)) })
		})
	}
}

func service(s *Store) *identity.Service { return identity.New(s.Identity(), security.Passwords{}) }
func initialize(t *testing.T, svc *identity.Service) identity.BootstrapResult {
	t.Helper()
	ctx := context.Background()
	token := identity.RandomToken("ogb_")
	if err := svc.PrepareBootstrap(ctx, token); err != nil {
		t.Fatal(err)
	}
	result, err := svc.Bootstrap(ctx, token, identity.BootstrapRequest{Username: "admin", Password: testPassword})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func login(t *testing.T, svc *identity.Service, password string) identity.LoginResult {
	t.Helper()
	result, err := svc.Login(context.Background(), identity.LoginRequest{Username: "admin", Password: password, Kind: "cli"})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func identityLifecycle(t *testing.T, s *Store) {
	ctx := context.Background()
	svc := service(s)
	initialize(t, svc)
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM project_api_key").Scan(&count); err != nil || count != 0 {
		t.Fatal("bootstrap issued a machine key")
	}
	first := login(t, svc, testPassword)
	result, err := svc.CreateAPIKey(ctx, first.Token, "cli", "RUN_ORCHESTRATOR")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.CreateAPIKey(ctx, result.APIKey, "key", "RUN_ORCHESTRATOR"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("machine key can issue keys")
	}
	if _, err := svc.Bootstrap(ctx, "wrong", identity.BootstrapRequest{Username: "admin", Password: testPassword}); !errors.Is(err, identity.ErrInitialized) {
		t.Fatalf("repeated bootstrap: %v", err)
	}
	keyPrincipal, err := svc.Authenticate(ctx, result.APIKey, "key")
	if err != nil || keyPrincipal.Role != "RUN_ORCHESTRATOR" || keyPrincipal.ProjectID != "default" {
		t.Fatalf("key principal: %+v %v", keyPrincipal, err)
	}
	if !slices.Equal(keyPrincipal.Scopes, []string{
		identity.ScopeRunsCreate,
		identity.ScopeRunsRead,
		identity.ScopeSubmissionCredentialRotate,
	}) || !keyPrincipal.Allows(identity.ScopeRunsCreate) ||
		!keyPrincipal.Allows(identity.ScopeRunsRead) ||
		!keyPrincipal.Allows(identity.ScopeSubmissionCredentialRotate) ||
		keyPrincipal.Allows(identity.ScopeRunsCreateWithRevision) {
		t.Fatalf("unexpected orchestrator grants: %+v", keyPrincipal)
	}
	missingPrincipal, err := svc.Authenticate(ctx, "ogk_missing", "key")
	if !errors.Is(err, identity.ErrUnauthorized) || missingPrincipal.Kind != "" ||
		missingPrincipal.User != nil || missingPrincipal.WorkspaceID != "" ||
		missingPrincipal.ProjectID != "" || missingPrincipal.Role != "" ||
		len(missingPrincipal.Scopes) != 0 || missingPrincipal.CredentialID != "" {
		t.Fatalf("failed key authentication returned principal data: %+v %v", missingPrincipal, err)
	}
	if _, err = svc.Authenticate(ctx, result.APIKey, "cli"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("machine key accepted as admin")
	}
	second := login(t, svc, testPassword)
	if _, err = svc.Authenticate(ctx, first.Token, "browser"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("CLI token accepted as browser session")
	}
	if err = svc.Logout(ctx, first.Token, "cli"); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Authenticate(ctx, first.Token, "cli"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("logged out token accepted")
	}
	if _, err = svc.CreateAPIKey(ctx, first.Token, "cli", "RUN_ORCHESTRATOR"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("revoked session issued key")
	}
	if err = svc.ChangePassword(ctx, second.Token, "cli", identity.PasswordRequest{CurrentPassword: testPassword, NewPassword: "new-synthetic-password"}); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Authenticate(ctx, second.Token, "cli"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("password change did not revoke session")
	}
	login(t, service(s), "new-synthetic-password")
	var hash string
	if err = s.db.QueryRow("SELECT password_hash FROM local_user").Scan(&hash); err != nil || !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatal("password not strongly hashed")
	}
	var events int
	if err = s.db.QueryRow("SELECT count(*) FROM audit_event").Scan(&events); err != nil || events < 7 {
		t.Fatalf("missing audit: %d %v", events, err)
	}
	var raw string
	if err = s.db.QueryRow("SELECT token_hash FROM project_api_key").Scan(&raw); err != nil || raw == result.APIKey {
		t.Fatal("key not hashed")
	}
	var auditedResource string
	if err = s.db.QueryRow("SELECT resource_id FROM audit_event WHERE event_type='api_key_created'").Scan(&auditedResource); err != nil || auditedResource != result.ID {
		t.Fatal("API key audit event is not linked to its stable ID")
	}
}
func bootstrapRace(t *testing.T, s *Store) {
	// Exercise independent physical connections, not SQLite's production pool limit.
	s.db.SetMaxOpenConns(4)
	s.db.SetMaxIdleConns(4)
	ctx := context.Background()
	svc := service(s)
	token := identity.RandomToken("ogb_")
	if err := svc.PrepareBootstrap(ctx, token); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := service(s).Bootstrap(ctx, token, identity.BootstrapRequest{Username: "admin", Password: testPassword})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, identity.ErrInitialized) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM local_user").Scan(&count); err != nil || count != 1 {
		t.Fatal("multiple administrators")
	}
}
func bootstrapRollback(t *testing.T, s *Store) {
	ctx := context.Background()
	svc := service(s)
	token := identity.RandomToken("ogb_")
	if err := svc.PrepareBootstrap(ctx, token); err != nil {
		t.Fatal(err)
	}
	// Inject a failure after all initialization writes, before transaction commit.
	failing := identity.New(failAuditRepository{Repository: s.Identity()}, security.Passwords{})
	if _, err := failing.Bootstrap(ctx, token, identity.BootstrapRequest{Username: "admin", Password: testPassword}); err == nil {
		t.Fatal("failed audit accepted")
	}
	initialized, err := svc.Status(ctx)
	if err != nil || initialized {
		t.Fatal("failed bootstrap changed initialized state")
	}
	for _, table := range []string{"workspace", "project", "local_user", "project_api_key"} {
		var count int
		if err = s.db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("partial initialization in %s: count=%d error=%v", table, count, err)
		}
	}
	if _, err = svc.Bootstrap(ctx, token, identity.BootstrapRequest{Username: "admin", Password: testPassword}); err != nil {
		t.Fatal(err)
	}
}

type failAuditRepository struct{ identity.Repository }

func (r failAuditRepository) Transact(ctx context.Context, fn func(identity.Transaction) error) error {
	return r.Repository.Transact(ctx, func(tx identity.Transaction) error { return fn(failAuditTransaction{tx}) })
}

type failAuditTransaction struct{ identity.Transaction }

func (tx failAuditTransaction) Audit(event, actor string) error {
	if event == "bootstrap_completed" {
		return identity.ErrUnavailable
	}
	return tx.Transaction.Audit(event, actor)
}
func (tx failAuditTransaction) AuditResource(event, actor, resourceType, resourceID string) error {
	if strings.HasPrefix(event, "api_key_") {
		return identity.ErrUnavailable
	}
	return tx.Transaction.AuditResource(event, actor, resourceType, resourceID)
}

type failSessionRepository struct{ identity.Repository }
type failSessionTransaction struct{ identity.Transaction }

func (r failSessionRepository) Transact(ctx context.Context, fn func(identity.Transaction) error) error {
	return r.Repository.Transact(ctx, func(tx identity.Transaction) error {
		return fn(failSessionTransaction{tx})
	})
}
func (tx failSessionTransaction) SaveSession(identity.Session) error { return identity.ErrUnavailable }

func localSetupSession(t *testing.T, s *Store) {
	ctx := context.Background()
	in := identity.BootstrapRequest{Username: "admin", Password: testPassword, SignIn: true}
	failing := identity.New(failSessionRepository{s.Identity()}, security.Passwords{})
	if result, err := failing.BootstrapLocal(ctx, in); err == nil || result.SessionToken != "" {
		t.Fatal("failed session creation returned success or a token")
	}
	svc := service(s)
	if initialized, err := svc.Status(ctx); err != nil || initialized {
		t.Fatal("failed session creation left an administrator initialized")
	}
	for _, table := range []string{"local_user", "admin_session", "workspace", "project", "project_api_key", "audit_event"} {
		var count int
		if err := s.db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("partial writes in %s", table)
		}
	}
	result, err := svc.BootstrapLocal(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if p, err := svc.Authenticate(ctx, result.SessionToken, "browser"); err != nil || p.User.ID != result.User.ID {
		t.Fatal("missing browser session")
	}
	if _, err := svc.Authenticate(ctx, result.SessionToken, "cli"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("browser session accepted as CLI")
	}
	if _, err := svc.BootstrapLocal(ctx, in); !errors.Is(err, identity.ErrInitialized) {
		t.Fatal("local setup repeated")
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM project_api_key").Scan(&count); err != nil || count != 0 {
		t.Fatal("local setup generated a machine key")
	}
}

func keyCreation(t *testing.T, s *Store) {
	ctx := context.Background()
	svc := service(s)
	initialize(t, svc)
	session := login(t, svc, testPassword)
	failing := identity.New(failAuditRepository{Repository: s.Identity()}, security.Passwords{})
	if result, err := failing.CreateAPIKey(ctx, session.Token, "cli", "RUN_ORCHESTRATOR"); err == nil || result.APIKey != "" {
		t.Fatal("key returned despite failed audit")
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM project_api_key").Scan(&count); err != nil || count != 0 {
		t.Fatal("key creation did not roll back")
	}
	first, err := svc.CreateAPIKey(ctx, session.Token, "cli", "RUN_ORCHESTRATOR")
	if err != nil {
		t.Fatal(err)
	}
	// Independent connections may create distinct keys without overwriting existing keys.
	s.db.SetMaxOpenConns(4)
	s.db.SetMaxIdleConns(4)
	results := make(chan error, 2)
	for range 2 {
		go func() { _, err := svc.CreateAPIKey(ctx, session.Token, "cli", "RUN_ORCHESTRATOR"); results <- err }()
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if err := s.db.QueryRow("SELECT count(*) FROM project_api_key").Scan(&count); err != nil || count != 3 {
		t.Fatalf("missing keys: count=%d error=%v", count, err)
	}
	if _, err := svc.Authenticate(ctx, first.APIKey, "key"); err != nil {
		t.Fatal("existing key invalidated")
	}
	scoped, err := svc.CreateAPIKey(ctx, session.Token, "cli", "RUN_ORCHESTRATOR", identity.ScopeRunsCreateWithRevision)
	if err != nil {
		t.Fatal(err)
	}
	scopedPrincipal, err := svc.Authenticate(ctx, scoped.APIKey, "key")
	if err != nil || !scopedPrincipal.Allows(identity.ScopeRunsCreateWithRevision) || !slices.Equal(scopedPrincipal.Scopes, []string{
		identity.ScopeRunsCreate,
		identity.ScopeRunsCreateWithRevision,
		identity.ScopeRunsRead,
		identity.ScopeSubmissionCredentialRotate,
	}) {
		t.Fatalf("unexpected scoped key grants: %+v error=%v", scopedPrincipal, err)
	}
	policyKey, err := svc.CreateAPIKey(ctx, session.Token, "cli", "POLICY_PUBLISHER")
	if err != nil {
		t.Fatal(err)
	}
	principal, err := svc.Authenticate(ctx, policyKey.APIKey, "key")
	if err != nil || !principal.Allows(identity.ScopePoliciesCreate) || !principal.Allows(identity.ScopeRevisionsPublish) || principal.Allows(identity.ScopeRunsCreate) || !slices.Equal(principal.Scopes, []string{
		identity.ScopePoliciesCreate,
		identity.ScopeRevisionsPublish,
	}) {
		t.Fatalf("unexpected policy key grants: role=%q error=%v", principal.Role, err)
	}
	if _, err := s.db.Exec("UPDATE project_api_key SET scopes_json=$1 WHERE token_hash=$2", `["runs:create"]`, identity.Digest(first.APIKey)); err != nil {
		t.Fatal(err)
	}
	if invalid, err := svc.Authenticate(ctx, first.APIKey, "key"); !errors.Is(err, identity.ErrCredentialState) || invalid.Kind != "" {
		t.Fatalf("incomplete persisted scopes authenticated: %+v error=%v", invalid, err)
	}
	if _, err := svc.CreateAPIKey(ctx, session.Token, "cli", "ADMIN"); !errors.Is(err, identity.ErrInvalid) {
		t.Fatal("administrator machine key role accepted")
	}
	if _, err := svc.CreateAPIKey(ctx, session.Token, "cli", "POLICY_PUBLISHER", identity.ScopeRunsCreateWithRevision); !errors.Is(err, identity.ErrInvalid) {
		t.Fatal("policy publisher accepted Run scope")
	}
	if _, err := svc.CreateAPIKey(ctx, session.Token, "cli", "RUN_ORCHESTRATOR", identity.ScopeRunsCreateWithRevision, identity.ScopeRunsCreateWithRevision); !errors.Is(err, identity.ErrInvalid) {
		t.Fatal("duplicate machine scope accepted")
	}
	svc.Now = func() time.Time { return time.Now().Add(identity.SessionLifetime + time.Second) }
	if _, err := svc.CreateAPIKey(ctx, session.Token, "cli", "RUN_ORCHESTRATOR"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("expired session issued a key")
	}
}

func identityExpiry(t *testing.T, s *Store) {
	ctx := context.Background()
	svc := service(s)
	now := time.Now()
	svc.Now = func() time.Time { return now }
	token := identity.RandomToken("ogb_")
	if err := svc.PrepareBootstrap(ctx, token); err != nil {
		t.Fatal(err)
	}
	now = now.Add(identity.BootstrapLifetime + time.Second)
	if _, err := svc.Bootstrap(ctx, token, identity.BootstrapRequest{Username: "admin", Password: testPassword}); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("expired bootstrap accepted")
	}
	if err := svc.PrepareBootstrap(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Bootstrap(ctx, token, identity.BootstrapRequest{Username: "admin", Password: testPassword}); err != nil {
		t.Fatal(err)
	}
	first := login(t, svc, testPassword)
	now = now.Add(identity.SessionLifetime + time.Second)
	if _, err := svc.Authenticate(ctx, first.Token, "cli"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("expired session accepted")
	}
	for i := 0; i < 5; i++ {
		_, err := svc.Login(ctx, identity.LoginRequest{Username: "admin", Password: "wrong", Kind: "cli"})
		if !errors.Is(err, identity.ErrUnauthorized) {
			t.Fatal(err)
		}
	}
	if _, err := svc.Login(ctx, identity.LoginRequest{Username: "admin", Password: testPassword, Kind: "cli"}); !errors.Is(err, identity.ErrRateLimited) {
		t.Fatalf("missing login budget: %v", err)
	}
	now = now.Add(31 * time.Second)
	login(t, svc, testPassword)
}
