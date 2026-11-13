// Package httpapi exposes the public HTTP contract and embedded management shell.
package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	contract "github.com/alibaba/open-guardrail/api/openapi"
	"github.com/alibaba/open-guardrail/internal/application/capability"
	"github.com/alibaba/open-guardrail/internal/buildinfo"
	"github.com/alibaba/open-guardrail/web"
)

const traceIDHeader = "X-Trace-ID"

type traceIDContextKey struct{}

var traceFallbackCounter atomic.Uint64

type Readiness interface {
	Ready(context.Context) error
	capability.Provider
}

type server struct {
	check          Readiness
	options        Options
	origin         *url.URL
	static         http.Handler
	localBootstrap bool
}

var _ contract.ServerInterface = (*server)(nil)

// These values describe the CLI protocol compatibility window, not the running
// server build version. They change only when the HTTP contract compatibility
// policy changes.
var publicCapabilities = contract.Capabilities{
	ApiVersions: []string{"v1alpha1"},
	Cli: contract.CLICompatibility{
		MinimumVersion:          "0.1.0-alpha.0",
		MaximumVersionExclusive: "0.2.0",
	},
	Features: []string{},
}

func New(check Readiness, options ...Options) http.Handler {
	s := &server{check: check, static: http.FileServerFS(web.Files())}
	if len(options) > 0 {
		s.options = options[0]
		s.origin, _ = url.Parse(options[0].Origin)
		s.localBootstrap = options[0].LocalBootstrap
	}
	mux := http.NewServeMux()
	contract.HandlerWithOptions(s, contract.StdHTTPServerOptions{
		BaseRouter:       mux,
		ErrorHandlerFunc: s.generatedRequestError,
	})
	// Reserve management namespaces: unknown routes must not accidentally become public.
	mux.HandleFunc("/api/v1alpha1/", s.handleUnknownManagementRoute)
	mux.HandleFunc("/", s.handlePage)
	var handler http.Handler = mux
	if s.options.ModelSpecs != nil || s.options.ModelRuns != nil {
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if s.handleModelAction(w, r) {
				return
			}
			mux.ServeHTTP(w, r)
		})
	}
	return securityHeaders(withRequestDeadline(requestBudget, withTraceID(withAccessLog(s.options.Logger, hideUnconfiguredRoutes(handler, s.options)))))
}

func hideUnconfiguredRoutes(next http.Handler, options Options) http.Handler {
	if options.Identity != nil && options.ModelSpecs != nil && options.ModelRuns != nil && options.Audits != nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Match stable API namespaces instead of enumerating generated routes. This
		// must run before generated parameter validation so disabled features stay
		// hidden as 404 even when a future endpoint adds required headers.
		auditRoute := r.URL.Path == "/api/v1alpha1/audit-events"
		identityRoute := r.URL.Path == "/api/v1alpha1/bootstrap" ||
			strings.HasPrefix(r.URL.Path, "/api/v1alpha1/bootstrap/") ||
			r.URL.Path == "/api/v1alpha1/auth" ||
			strings.HasPrefix(r.URL.Path, "/api/v1alpha1/auth/") ||
			r.URL.Path == "/runtime/v1alpha1/identity" ||
			strings.HasPrefix(r.URL.Path, "/runtime/v1alpha1/identity/")
		specRoute := strings.HasPrefix(r.URL.Path, "/api/v1alpha1/scenes") ||
			strings.HasPrefix(r.URL.Path, "/api/v1alpha1/policies/") ||
			strings.HasPrefix(r.URL.Path, "/api/v1alpha1/policy-revisions/")
		runRoute := r.URL.Path == "/runtime/v1alpha1/runs" || strings.HasPrefix(r.URL.Path, "/runtime/v1alpha1/runs/") ||
			r.URL.Path == "/api/v1alpha1/runs" || strings.HasPrefix(r.URL.Path, "/api/v1alpha1/runs/")
		if (auditRoute && (options.Identity == nil || options.Audits == nil)) || (identityRoute && options.Identity == nil) || (specRoute && options.ModelSpecs == nil) || (runRoute && options.ModelRuns == nil) {
			writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) GetLiveness(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, contract.Health{Status: contract.Alive})
}

func (s *server) GetReadiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.check.Ready(ctx); err != nil {
		writeError(w, r, http.StatusServiceUnavailable, "NOT_READY", "Service is not ready")
		return
	}
	writeJSON(w, http.StatusOK, contract.Health{Status: contract.Ready})
}

func (s *server) GetVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, contract.Version{Version: buildinfo.Public().Version})
}

func (s *server) GetCapabilities(w http.ResponseWriter, r *http.Request) {
	response := publicCapabilities
	response.Features = []string{}
	if s.options.Identity != nil {
		response.Features = append(response.Features, "local-administrator", "project-api-key")
		if s.options.Audits != nil {
			response.Features = append(response.Features, "project-audit-query")
		}
		if s.options.ModelSpecs != nil {
			response.Features = append(response.Features, "scene-policy-management", "immutable-policy-revisions", "resource-policy-bindings")
		}
		if s.options.ModelRuns != nil {
			response.Features = append(response.Features, "policy-revision-runs", "explicit-revision-run-scope", "multi-submission-decision", "run-expiry-decision-validity", "run-scoped-submission-token", "run-diagnostic-report")
		}
	}
	storage := s.check.StorageCapability()
	supportLevel, ok := contractStorageSupportLevel(storage.SupportLevel)
	if !ok {
		writeError(w, r, http.StatusServiceUnavailable, "CAPABILITY_UNAVAILABLE", "Storage capability is unavailable")
		return
	}
	response.Storage = contract.StorageCapability{
		SupportLevel:  supportLevel,
		MultiInstance: storage.MultiInstance,
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *server) GetOpenAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	_, _ = w.Write(contract.Document)
}

func (s *server) handlePage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	switch r.URL.Path {
	case "/", "/index.html", "/app.js", "/i18n.js", "/specs.js", "/runs.js", "/audit.js", "/export.js", "/console.js", "/editor.js", "/style.css", "/select-arrow.svg", "/favicon.svg":
		s.static.ServeHTTP(w, r)
	case "/login", "/setup":
		r.URL.Path = "/"
		s.static.ServeHTTP(w, r)
	default:
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
	}
}

func contractStorageSupportLevel(level capability.SupportLevel) (contract.StorageCapabilitySupportLevel, bool) {
	switch level {
	case capability.SupportOfficialLocal:
		return contract.OfficialLocal, true
	case capability.SupportOfficialShared:
		return contract.OfficialShared, true
	default:
		return "", false
	}
}

func (s *server) generatedRequestError(w http.ResponseWriter, r *http.Request, err error) {
	paramName := ""
	var required *contract.RequiredHeaderError
	var invalid *contract.InvalidParamFormatError
	var multiple *contract.TooManyValuesForParamError
	switch {
	case errors.As(err, &required):
		paramName = required.ParamName
	case errors.As(err, &invalid):
		paramName = invalid.ParamName
	case errors.As(err, &multiple):
		paramName = multiple.ParamName
	}
	if paramName == "X-Guardrail-CSRF" {
		if !s.trustedOrigin(w, r) {
			return
		}
		writeError(w, r, http.StatusForbidden, "CSRF_REQUIRED", "Custom request header required")
		return
	}
	writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "Invalid request")
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; object-src 'none'; base-uri 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func withTraceID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceID := newTraceID()
		w.Header().Set(traceIDHeader, traceID)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), traceIDContextKey{}, traceID)))
	})
}

func newTraceID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err == nil {
		return hex.EncodeToString(value[:])
	}
	seed := strconv.FormatInt(time.Now().UnixNano(), 10) + ":" + strconv.FormatUint(traceFallbackCounter.Add(1), 10)
	digest := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(digest[:16])
}

func requestTraceID(r *http.Request) string {
	if value, ok := r.Context().Value(traceIDContextKey{}).(string); ok {
		return value
	}
	return newTraceID()
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	writeJSON(w, status, contract.Error{Code: code, Message: message, TraceId: requestTraceID(r)})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
