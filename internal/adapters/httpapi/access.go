package httpapi

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/alibaba/open-guardrail/internal/application/audit"
	runapp "github.com/alibaba/open-guardrail/internal/application/run"
	"github.com/alibaba/open-guardrail/internal/application/spec"
)

type Options struct {
	Audits                   *audit.Service
	Logger                   *slog.Logger
	TrustedOperatorNamespace string
	ModelSpecs               *spec.Service
	ModelRuns                *runapp.Service
	Origin                   string
}

func (s *server) guard(w http.ResponseWriter, r *http.Request) bool {
	if s.options.ModelSpecs == nil && s.options.ModelRuns == nil && s.options.Audits == nil {
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
		return false
	}
	if !s.trustedOrigin(w, r) {
		return false
	}
	if s.options.TrustedOperatorNamespace == "" && r.Header.Get("Authorization") != "" {
		writeError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Run credentials are accepted only for submissions")
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

func credential(r *http.Request) (string, string) {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ogrt_") {
		return strings.TrimPrefix(auth, "Bearer "), "run_token"
	}
	return "", ""
}

func (s *server) handleUnknownManagementRoute(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
}
