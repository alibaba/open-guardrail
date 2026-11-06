// Package identity owns the single-administrator lifecycle and credential boundaries.
package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"regexp"
	"sort"
	"time"
	"unicode/utf8"
)

var (
	ErrRateLimited     = errors.New("login temporarily limited")
	ErrInvalid         = errors.New("invalid request")
	ErrUnauthorized    = errors.New("invalid or expired credentials")
	ErrCredentialState = errors.New("stored credential state is invalid")
	ErrInitialized     = errors.New("installation already initialized")
	ErrUnavailable     = errors.New("identity operation unavailable")
)

const SessionLifetime = 8 * time.Hour
const BootstrapLifetime = time.Hour

type User struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	WorkspaceID  string `json:"workspaceId"`
	ProjectID    string `json:"projectId"`
	PasswordHash string `json:"-"`
}
type State struct {
	Initialized      bool
	BootstrapHash    string
	BootstrapExpires int64
	LoginAttempts    int64
	LoginRetryAt     int64
	LoginLastAttempt int64
}
type Session struct {
	Hash, UserID, Kind string
	Expires            int64
}
type Principal struct {
	Kind         string   `json:"kind"`
	User         *User    `json:"user,omitempty"`
	WorkspaceID  string   `json:"workspaceId"`
	ProjectID    string   `json:"projectId"`
	Role         string   `json:"role"`
	Scopes       []string `json:"scopes"`
	CredentialID string   `json:"-"`
}

const (
	ScopePoliciesCreate             = "policies:create"
	ScopeRevisionsPublish           = "revisions:publish"
	ScopeRunsCreate                 = "runs:create"
	ScopeRunsCreateWithRevision     = "runs:create-with-revision"
	ScopeRunsRead                   = "runs:read"
	ScopeSubmissionCredentialRotate = "submission-credential:rotate"
)

// Machine roles are fixed Scope templates; authorization checks only effective Scopes.
func (p Principal) Allows(scope string) bool {
	if p.Kind != "api_key" {
		return false
	}
	for _, granted := range p.Scopes {
		if granted == scope {
			return true
		}
	}
	return false
}

type machineScopeTemplate struct {
	Required []string
	Optional []string
}

func machineTemplate(role string) (machineScopeTemplate, bool) {
	switch role {
	case "POLICY_PUBLISHER":
		return machineScopeTemplate{Required: []string{ScopePoliciesCreate, ScopeRevisionsPublish}}, true
	case "RUN_ORCHESTRATOR":
		return machineScopeTemplate{
			Required: []string{ScopeRunsCreate, ScopeRunsRead, ScopeSubmissionCredentialRotate},
			Optional: []string{ScopeRunsCreateWithRevision},
		}, true
	default:
		return machineScopeTemplate{}, false
	}
}

type BootstrapRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	SignIn   bool   `json:"signIn,omitempty"`
}
type BootstrapResult struct {
	User User `json:"user"`
	// Delivered only as an HttpOnly cookie, never in the JSON response.
	SessionToken string `json:"-"`
}
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Kind     string `json:"kind"`
}
type LoginResult struct {
	Token     string `json:"token,omitempty"`
	ExpiresAt int64  `json:"expiresAt"`
	User      User   `json:"user"`
}
type PasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}
type Passwords interface {
	Hash(string) (string, error)
	Verify(string, string) bool
}

// Transaction serializes all identity writes using a database row lock.
// Returning an error rolls back; failed authentication counters are committed separately.
type Transaction interface {
	State() (State, error)
	SaveState(State) error
	User() (User, error)
	SaveUser(User) error
	Initialize(User) error
	CreateAPIKey(User, string, APIKeyMetadata) error
	APIKey(User, string) (APIKeyMetadata, error)
	APIKeys(User, string, int) ([]APIKeyMetadata, error)
	ReplaceAPIKey(User, APIKeyMetadata, string) error
	RevokeAPIKey(User, string, int64) error
	Session(string) (Session, error)
	SaveSession(Session) error
	DeleteSession(string) error
	DeleteUserSessions(string) error
	Audit(string, string) error
	AuditResource(string, string, string, string) error
}
type Repository interface {
	Transact(context.Context, func(Transaction) error) error
	Status(context.Context) (State, error)
	Authenticate(context.Context, string, string, func() int64) (Principal, error)
}
type Service struct {
	Repo       Repository
	Passwords  Passwords
	Now        func() time.Time
	loginSlots chan struct{}
}

func New(repo Repository, passwords Passwords) *Service {
	return &Service{Repo: repo, Passwords: passwords, Now: time.Now, loginSlots: make(chan struct{}, 2)}
}

// Two independent random strings provide at least 256 bits of token entropy.
func RandomToken(prefix string) string { return prefix + rand.Text() + rand.Text() }
func Digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func equal(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{2,63}$`)

// Preview-only local password policy; no character-composition requirements.
func ValidPassword(p string) bool {
	return utf8.ValidString(p) && utf8.RuneCountInString(p) >= 6 && len(p) <= 128
}
func (s *Service) Status(ctx context.Context) (bool, error) {
	st, err := s.Repo.Status(ctx)
	return st.Initialized, err
}

// PrepareBootstrap is invoked only by the server on the service host, never by HTTP.
func (s *Service) PrepareBootstrap(ctx context.Context, token string) error {
	return s.Repo.Transact(ctx, func(tx Transaction) error {
		st, err := tx.State()
		if err != nil {
			return err
		}
		if st.Initialized {
			return ErrInitialized
		}
		now := s.Now().Unix()
		if equal(st.BootstrapHash, Digest(token)) && st.BootstrapExpires > now {
			return nil
		}
		st.BootstrapHash = Digest(token)
		st.BootstrapExpires = now + int64(BootstrapLifetime.Seconds())
		if err = tx.SaveState(st); err != nil {
			return err
		}
		return tx.Audit("bootstrap_token_prepared", "local-host")
	})
}
func (s *Service) Bootstrap(ctx context.Context, token string, in BootstrapRequest) (BootstrapResult, error) {
	return s.bootstrap(ctx, token, in, false)
}

// BootstrapLocal must only be called by an adapter that has verified direct,
// loopback-only deployment and request transport. This is never a request field.
func (s *Service) BootstrapLocal(ctx context.Context, in BootstrapRequest) (BootstrapResult, error) {
	return s.bootstrap(ctx, "", in, true)
}

func (s *Service) bootstrap(ctx context.Context, token string, in BootstrapRequest, local bool) (BootstrapResult, error) {
	var out BootstrapResult
	if !usernamePattern.MatchString(in.Username) || !ValidPassword(in.Password) {
		return out, ErrInvalid
	}
	var outcome error
	err := s.Repo.Transact(ctx, func(tx Transaction) error {
		st, err := tx.State()
		if err != nil {
			return err
		}
		if st.Initialized {
			return ErrInitialized
		}
		now := s.Now().Unix()
		if !local && (st.BootstrapExpires <= now || !equal(st.BootstrapHash, Digest(token))) {
			outcome = ErrUnauthorized
			return tx.Audit("bootstrap_failed", "anonymous")
		}
		hash, err := s.Passwords.Hash(in.Password)
		if err != nil {
			return ErrUnavailable
		}
		user := User{ID: "admin", Username: in.Username, PasswordHash: hash, WorkspaceID: "default", ProjectID: "default"}
		if err = tx.Initialize(user); err != nil {
			return err
		}
		st.Initialized = true
		st.BootstrapHash = ""
		st.BootstrapExpires = 0
		if err = tx.SaveState(st); err != nil {
			return err
		}
		if err = tx.Audit("bootstrap_completed", user.ID); err != nil {
			return err
		}
		out = BootstrapResult{User: user}
		if in.SignIn {
			out.SessionToken = RandomToken("ogs_")
			if err = tx.SaveSession(Session{Hash: Digest(out.SessionToken), UserID: user.ID, Kind: "browser", Expires: now + int64(SessionLifetime.Seconds())}); err != nil {
				return err
			}
			return tx.Audit("login_succeeded", user.ID)
		}
		return nil
	})
	if err != nil {
		return BootstrapResult{}, err
	}
	return out, outcome
}

// resolveMachineScopes expands a role template plus the explicitly requestable grant.
func resolveMachineScopes(role string, requested []string) ([]string, error) {
	template, ok := machineTemplate(role)
	if !ok {
		return nil, ErrInvalid
	}
	scopes := append([]string(nil), template.Required...)
	seen := make(map[string]bool, len(scopes)+len(requested))
	for _, scope := range scopes {
		seen[scope] = true
	}
	for _, scope := range requested {
		if !containsScope(template.Optional, scope) || seen[scope] {
			return nil, ErrInvalid
		}
		seen[scope] = true
		scopes = append(scopes, scope)
	}
	sort.Strings(scopes)
	return scopes, nil
}

// ValidateMachineScopes rejects missing, unknown or duplicate persisted grants.
// Persisting complete templates freezes the permissions issued to each key.
func ValidateMachineScopes(role string, scopes []string) ([]string, error) {
	template, ok := machineTemplate(role)
	if !ok {
		return nil, ErrInvalid
	}
	allowed := make(map[string]bool, len(template.Required)+len(template.Optional))
	for _, scope := range template.Required {
		allowed[scope] = true
	}
	for _, scope := range template.Optional {
		allowed[scope] = true
	}
	seen := make(map[string]bool, len(scopes))
	for _, scope := range scopes {
		if !allowed[scope] || seen[scope] {
			return nil, ErrInvalid
		}
		seen[scope] = true
	}
	for _, scope := range template.Required {
		if !seen[scope] {
			return nil, ErrInvalid
		}
	}
	canonical := append([]string(nil), scopes...)
	sort.Strings(canonical)
	return canonical, nil
}

func containsScope(scopes []string, target string) bool {
	for _, scope := range scopes {
		if scope == target {
			return true
		}
	}
	return false
}

// Login reserves a bounded attempt in shared storage before expensive work.
// Password verification runs outside the identity transaction; its snapshot is
// rechecked before issuing a session so password reset wins over an in-flight login.
func (s *Service) Login(ctx context.Context, in LoginRequest) (LoginResult, error) {
	if len(in.Username) > 64 || len(in.Password) > 128 || (in.Kind != "browser" && in.Kind != "cli") {
		return LoginResult{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return LoginResult{}, err
	}
	select {
	case s.loginSlots <- struct{}{}:
		defer func() { <-s.loginSlots }()
	default:
		return LoginResult{}, ErrRateLimited
	}
	var snapshot User
	var outcome error
	err := s.Repo.Transact(ctx, func(tx Transaction) error {
		st, err := tx.State()
		if err != nil {
			return err
		}
		if !st.Initialized {
			outcome = ErrUnauthorized
			return tx.Audit("login_failed", "anonymous")
		}
		now := s.Now().Unix()
		if st.LoginRetryAt > now {
			return ErrRateLimited
		}
		if now-st.LoginLastAttempt >= 900 {
			st.LoginAttempts = 0
		}
		if st.LoginAttempts < 10 {
			st.LoginAttempts++
		}
		st.LoginLastAttempt = now
		st.LoginRetryAt = 0
		if st.LoginAttempts >= 5 {
			delay := int64(1) << (st.LoginAttempts - 5)
			if delay > 30 {
				delay = 30
			}
			st.LoginRetryAt = now + delay
		}
		if err := tx.SaveState(st); err != nil {
			return err
		}
		snapshot, err = tx.User()
		return err
	})
	if err != nil {
		return LoginResult{}, err
	}
	if outcome != nil {
		return LoginResult{}, outcome
	}
	valid := s.Passwords.Verify(snapshot.PasswordHash, in.Password) && equal(snapshot.Username, in.Username)
	var out LoginResult
	err = s.Repo.Transact(ctx, func(tx Transaction) error {
		user, err := tx.User()
		if err != nil {
			return err
		}
		if !valid || !equal(user.PasswordHash, snapshot.PasswordHash) || !equal(user.Username, snapshot.Username) {
			outcome = ErrUnauthorized
			return tx.Audit("login_failed", "anonymous")
		}
		st, err := tx.State()
		if err != nil {
			return err
		}
		st.LoginAttempts, st.LoginRetryAt, st.LoginLastAttempt = 0, 0, 0
		if err := tx.SaveState(st); err != nil {
			return err
		}
		token := RandomToken("ogs_")
		expires := s.Now().Unix() + int64(SessionLifetime.Seconds())
		if err := tx.SaveSession(Session{Hash: Digest(token), UserID: user.ID, Kind: in.Kind, Expires: expires}); err != nil {
			return err
		}
		if err := tx.Audit("login_succeeded", user.ID); err != nil {
			return err
		}
		out = LoginResult{Token: token, ExpiresAt: expires, User: user}
		return nil
	})
	if err != nil {
		return LoginResult{}, err
	}
	return out, outcome
}
func (s *Service) Authenticate(ctx context.Context, token, kind string) (Principal, error) {
	if token == "" || len(token) > 256 {
		return Principal{}, ErrUnauthorized
	}
	return s.Repo.Authenticate(ctx, Digest(token), kind, func() int64 { return s.Now().Unix() })
}
func (s *Service) withSession(ctx context.Context, token, kind string, fn func(Transaction, User) error) error {
	return s.Repo.Transact(ctx, func(tx Transaction) error {
		session, err := tx.Session(Digest(token))
		if err != nil {
			return err
		}
		if session.Kind != kind || session.Expires <= s.Now().Unix() {
			return ErrUnauthorized
		}
		user, err := tx.User()
		if err != nil {
			return err
		}
		if user.ID != session.UserID {
			return ErrUnauthorized
		}
		return fn(tx, user)
	})
}
func (s *Service) Logout(ctx context.Context, token, kind string) error {
	return s.withSession(ctx, token, kind, func(tx Transaction, user User) error {
		if err := tx.DeleteSession(Digest(token)); err != nil {
			return err
		}
		return tx.Audit("logout", user.ID)
	})
}
func (s *Service) ChangePassword(ctx context.Context, token, kind string, in PasswordRequest) error {
	if !ValidPassword(in.NewPassword) || len(in.CurrentPassword) > 128 {
		return ErrInvalid
	}
	var outcome error
	err := s.withSession(ctx, token, kind, func(tx Transaction, user User) error {
		if !s.Passwords.Verify(user.PasswordHash, in.CurrentPassword) {
			outcome = ErrUnauthorized
			return tx.Audit("password_change_failed", user.ID)
		}
		return s.setPassword(tx, user, in.NewPassword)
	})
	if err != nil {
		return err
	}
	return outcome
}

func (s *Service) setPassword(tx Transaction, user User, password string) error {
	hash, err := s.Passwords.Hash(password)
	if err != nil {
		return ErrUnavailable
	}
	user.PasswordHash = hash
	if err = tx.SaveUser(user); err != nil {
		return err
	}
	st, err := tx.State()
	if err != nil {
		return err
	}
	st.LoginAttempts, st.LoginRetryAt, st.LoginLastAttempt = 0, 0, 0
	if err := tx.SaveState(st); err != nil {
		return err
	}
	if err = tx.DeleteUserSessions(user.ID); err != nil {
		return err
	}
	return tx.Audit("password_changed", user.ID)
}
