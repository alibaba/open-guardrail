package httpapi

import (
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"

	contract "github.com/alibaba/open-guardrail/api/openapi"
	"github.com/alibaba/open-guardrail/internal/application/audit"
	"github.com/alibaba/open-guardrail/internal/application/identity"
	runapp "github.com/alibaba/open-guardrail/internal/application/run"
	"github.com/alibaba/open-guardrail/internal/application/spec"
)

type Options struct {
	Audits     *audit.Service
	Logger     *slog.Logger
	Identity   *identity.Service
	ModelSpecs *spec.Service
	ModelRuns  *runapp.Service
	Origin     string
	// Set only by server wiring after inspecting the actual bound listener.
	LocalBootstrap bool
}

func (s *server) identityError(w http.ResponseWriter, r *http.Request, err error) {
	code, message, status := "IDENTITY_UNAVAILABLE", "Identity service is unavailable", http.StatusServiceUnavailable
	switch {
	case errors.Is(err, identity.ErrKeyNotFound):
		code, message, status = "KEY_NOT_FOUND", "Machine key not found", http.StatusNotFound
	case errors.Is(err, identity.ErrKeyConflict):
		code, message, status = "KEY_STATE_CONFLICT", "Machine key changed, expired or revoked", http.StatusConflict
	case errors.Is(err, identity.ErrRateLimited):
		w.Header().Set("Retry-After", "30")
		code, message, status = "RATE_LIMITED", "Login temporarily limited; retry shortly", http.StatusTooManyRequests
	case errors.Is(err, identity.ErrInvalid):
		code, message, status = "INVALID_ARGUMENT", "Invalid identity request", http.StatusBadRequest
	case errors.Is(err, identity.ErrUnauthorized):
		code, message, status = "UNAUTHENTICATED", "Authentication required or credential rejected", http.StatusUnauthorized
	case errors.Is(err, identity.ErrCredentialState):
		code, message, status = "UNAUTHENTICATED", "Authentication required or credential rejected", http.StatusUnauthorized
		if s.options.Logger != nil {
			s.options.Logger.WarnContext(r.Context(), "stored machine credential state rejected", "trace_id", requestTraceID(r))
		}
	case errors.Is(err, identity.ErrInitialized):
		code, message, status = "ALREADY_INITIALIZED", "Administrator is already initialized", http.StatusConflict
	}
	writeError(w, r, status, code, message)
}

func (s *server) decode(w http.ResponseWriter, r *http.Request, value any) bool {
	if !decodeJSON(w, r, value, 4096) {
		s.identityError(w, r, identity.ErrInvalid)
		return false
	}
	return true
}

func credential(r *http.Request) (string, string) {
	auth := r.Header.Get("Authorization")
	cookie, err := r.Cookie("og_session")
	if auth != "" {
		if err == nil || !strings.HasPrefix(auth, "Bearer ") {
			return "", ""
		}
		token := strings.TrimPrefix(auth, "Bearer ")
		if strings.HasPrefix(token, "ogk_") {
			return token, "key"
		}
		if strings.HasPrefix(token, "ogrt_") {
			return token, "run_token"
		}
		return token, "cli"
	}
	if err == nil {
		return cookie.Value, "browser"
	}
	return "", ""
}
func (s *server) guard(w http.ResponseWriter, r *http.Request) bool {
	if s.options.Identity == nil {
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
		return false
	}
	if !s.trustedOrigin(w, r) {
		return false
	}
	// Generated wrappers enforce this header on declared write operations. Keep
	// the guard as defense in depth for hand-written or fallback routes so a
	// future route cannot silently bypass the same-origin write requirement.
	if r.Method != http.MethodGet && r.Header.Get("X-Guardrail-CSRF") != "1" {
		writeError(w, r, http.StatusForbidden, "CSRF_REQUIRED", "Custom request header required")
		return false
	}
	return true
}

func (s *server) trustedOrigin(w http.ResponseWriter, r *http.Request) bool {
	if s.origin == nil || r.Host != s.origin.Host {
		writeError(w, r, http.StatusForbidden, "UNTRUSTED_ORIGIN", "Untrusted host")
		return false
	}
	if value := r.Header.Get("Origin"); value != "" && value != s.options.Origin {
		writeError(w, r, http.StatusForbidden, "UNTRUSTED_ORIGIN", "Cross-origin request denied")
		return false
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		writeError(w, r, http.StatusForbidden, "UNTRUSTED_ORIGIN", "Cross-site request denied")
		return false
	}
	return true
}

func (s *server) localSetup(r *http.Request) bool {
	if !s.localBootstrap || s.origin == nil || s.origin.Scheme != "http" {
		return false
	}
	ip := net.ParseIP(s.origin.Hostname())
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || ip == nil || !ip.IsLoopback() || !net.ParseIP(peer).IsLoopback() {
		return false
	}
	// Reverse proxies must declare public_url; forwarded requests cannot opt in.
	for key := range r.Header {
		key = strings.ToLower(key)
		if key == "forwarded" || strings.HasPrefix(key, "x-forwarded-") {
			return false
		}
	}
	return true
}

func (s *server) setSessionCookie(w http.ResponseWriter, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     "og_session",
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.origin != nil && s.origin.Scheme == "https",
		SameSite: http.SameSiteStrictMode,
		MaxAge:   maxAge,
	})
}

func (s *server) GetBootstrapStatus(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	initialized, err := s.options.Identity.Status(r.Context())
	if err != nil {
		s.identityError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, contract.BootstrapStatus{Initialized: initialized, RequiresToken: !s.localSetup(r)})
}

func (s *server) BootstrapAdministrator(w http.ResponseWriter, r *http.Request, _ contract.BootstrapAdministratorParams) {
	if !s.guard(w, r) {
		return
	}
	var input contract.BootstrapRequest
	if !s.decode(w, r, &input) {
		return
	}
	if input.Password == nil {
		s.identityError(w, r, identity.ErrInvalid)
		return
	}
	signIn := input.SignIn != nil && *input.SignIn
	request := identity.BootstrapRequest{Username: input.Username, Password: *input.Password, SignIn: signIn}
	var result identity.BootstrapResult
	var err error
	auth := r.Header.Get("Authorization")
	if auth == "" && s.localSetup(r) {
		result, err = s.options.Identity.BootstrapLocal(r.Context(), request)
	} else {
		if !strings.HasPrefix(auth, "Bearer ogb_") {
			s.identityError(w, r, identity.ErrUnauthorized)
			return
		}
		result, err = s.options.Identity.Bootstrap(r.Context(), strings.TrimPrefix(auth, "Bearer "), request)
	}
	if err != nil {
		s.identityError(w, r, err)
		return
	}
	if result.SessionToken != "" {
		s.setSessionCookie(w, result.SessionToken, int(identity.SessionLifetime.Seconds()))
	}
	writeJSON(w, http.StatusCreated, contract.BootstrapResult{User: contractUser(result.User)})
}

func (s *server) LoginAdministrator(w http.ResponseWriter, r *http.Request, _ contract.LoginAdministratorParams) {
	if !s.guard(w, r) {
		return
	}
	var input contract.LoginRequest
	if !s.decode(w, r, &input) {
		return
	}
	if input.Password == nil {
		s.identityError(w, r, identity.ErrInvalid)
		return
	}
	kind := "browser"
	if input.Kind != nil {
		if !input.Kind.Valid() {
			s.identityError(w, r, identity.ErrInvalid)
			return
		}
		kind = string(*input.Kind)
	}
	// A browser may not request a bearer credential readable by JavaScript.
	if kind == "cli" && (r.Header.Get("Origin") != "" || r.Header.Get("Sec-Fetch-Site") != "") {
		s.identityError(w, r, identity.ErrInvalid)
		return
	}
	result, err := s.options.Identity.Login(r.Context(), identity.LoginRequest{
		Username: input.Username,
		Password: *input.Password,
		Kind:     kind,
	})
	if err != nil {
		s.identityError(w, r, err)
		return
	}
	response := contract.LoginResult{ExpiresAt: result.ExpiresAt, User: contractUser(result.User)}
	if kind == "browser" {
		s.setSessionCookie(w, result.Token, int(identity.SessionLifetime.Seconds()))
	} else {
		response.Token = &result.Token
	}
	writeJSON(w, http.StatusCreated, response)
}

func (s *server) GetCurrentAdministrator(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	token, kind := credential(r)
	if kind == "key" {
		s.identityError(w, r, identity.ErrUnauthorized)
		return
	}
	principal, err := s.options.Identity.Authenticate(r.Context(), token, kind)
	if err != nil {
		s.identityError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, contractPrincipal(principal))
}

func (s *server) LogoutAdministrator(w http.ResponseWriter, r *http.Request, _ contract.LogoutAdministratorParams) {
	if !s.guard(w, r) {
		return
	}
	token, kind := credential(r)
	if kind == "key" {
		s.identityError(w, r, identity.ErrUnauthorized)
		return
	}
	if err := s.options.Identity.Logout(r.Context(), token, kind); err != nil {
		s.identityError(w, r, err)
		return
	}
	s.setSessionCookie(w, "", -1)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) ChangeAdministratorPassword(w http.ResponseWriter, r *http.Request, _ contract.ChangeAdministratorPasswordParams) {
	if !s.guard(w, r) {
		return
	}
	token, kind := credential(r)
	if kind == "key" {
		s.identityError(w, r, identity.ErrUnauthorized)
		return
	}
	var input contract.PasswordRequest
	if !s.decode(w, r, &input) {
		return
	}
	if input.CurrentPassword == nil || input.NewPassword == nil {
		s.identityError(w, r, identity.ErrInvalid)
		return
	}
	request := identity.PasswordRequest{CurrentPassword: *input.CurrentPassword, NewPassword: *input.NewPassword}
	if err := s.options.Identity.ChangePassword(r.Context(), token, kind, request); err != nil {
		s.identityError(w, r, err)
		return
	}
	s.setSessionCookie(w, "", -1)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) CreateProjectAPIKey(w http.ResponseWriter, r *http.Request, _ contract.CreateProjectAPIKeyParams) {
	if !s.guard(w, r) {
		return
	}
	var input contract.CreateAPIKeyRequest
	if !s.decode(w, r, &input) {
		return
	}
	role := ""
	if input.Role != nil {
		role = string(*input.Role)
	}
	scopes := []string{}
	if input.Scopes != nil {
		for _, scope := range *input.Scopes {
			scopes = append(scopes, string(scope))
		}
	}
	token, kind := credential(r)
	result, err := s.options.Identity.IssueAPIKey(r.Context(), token, kind, identity.CreateAPIKeyInput{
		Role: role, Scopes: scopes, ExpiresInSeconds: input.ExpiresInSeconds,
	})
	if err != nil {
		s.identityError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, contractAPIKeyResult(result))
}

func (s *server) GetMachineIdentity(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	token, kind := credential(r)
	if kind != "key" {
		s.identityError(w, r, identity.ErrUnauthorized)
		return
	}
	principal, err := s.options.Identity.Authenticate(r.Context(), token, kind)
	if err != nil {
		s.identityError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, contractPrincipal(principal))
}

func (s *server) handleUnknownManagementRoute(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	token, kind := credential(r)
	if kind == "key" {
		s.identityError(w, r, identity.ErrUnauthorized)
		return
	}
	if _, err := s.options.Identity.Authenticate(r.Context(), token, kind); err != nil {
		s.identityError(w, r, err)
		return
	}
	writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
}

func contractUser(user identity.User) contract.User {
	return contract.User{
		Id:          user.ID,
		Username:    user.Username,
		WorkspaceId: user.WorkspaceID,
		ProjectId:   user.ProjectID,
	}
}

func contractPrincipal(principal identity.Principal) contract.Principal {
	result := contract.Principal{
		Kind:        contract.PrincipalKind(principal.Kind),
		Role:        contract.PrincipalRole(principal.Role),
		Scopes:      make([]contract.MachineScope, 0, len(principal.Scopes)),
		WorkspaceId: principal.WorkspaceID,
		ProjectId:   principal.ProjectID,
	}
	for _, scope := range principal.Scopes {
		result.Scopes = append(result.Scopes, contract.MachineScope(scope))
	}
	if principal.User != nil {
		user := contractUser(*principal.User)
		result.User = &user
	}
	return result
}
