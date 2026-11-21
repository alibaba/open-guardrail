package httpapi

import (
	runapp "github.com/alibaba/open-guardrail/internal/application/run"
	"github.com/alibaba/open-guardrail/internal/application/spec"
	"net/http"
)

func (s *server) specScope(w http.ResponseWriter, r *http.Request) (spec.Scope, bool) {
	if !s.guard(w, r) || s.options.ModelSpecs == nil {
		return spec.Scope{}, false
	}
	if s.options.TrustedOperatorNamespace != "" {
		return s.trustedOperatorScope(w, r)
	}
	return spec.Scope{WorkspaceID: "default", ProjectID: "default", ActorID: "local", Unrestricted: true}, true
}

func (s *server) runScope(w http.ResponseWriter, r *http.Request) (runapp.Scope, bool) {
	if s.options.ModelRuns == nil {
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
		return runapp.Scope{}, false
	}
	if s.options.TrustedOperatorNamespace != "" {
		scope, ok := s.trustedOperatorScope(w, r)
		return runapp.Scope{WorkspaceID: scope.WorkspaceID, ProjectID: scope.ProjectID,
			ActorID: scope.ActorID, Operator: scope.Operator, CanSelectRevision: true}, ok
	}
	if !s.guard(w, r) {
		return runapp.Scope{}, false
	}
	return runapp.Scope{WorkspaceID: "default", ProjectID: "default", ActorID: "local", CanSelectRevision: true}, true
}

func (s *server) managedRunScope(w http.ResponseWriter, r *http.Request) (runapp.Scope, bool) {
	if !s.guard(w, r) {
		return runapp.Scope{}, false
	}
	return s.runScope(w, r)
}

func (s *server) submissionScope(w http.ResponseWriter, r *http.Request, runID string) (runapp.Scope, bool) {
	if s.options.ModelRuns == nil {
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
		return runapp.Scope{}, false
	}
	token, kind := credential(r)
	if kind != "run_token" {
		s.modelRunError(w, r, runapp.ErrUnauthorized)
		return runapp.Scope{}, false
	}
	scope, err := s.options.ModelRuns.Authenticate(r.Context(), runID, token)
	if err != nil {
		s.modelRunError(w, r, err)
		return runapp.Scope{}, false
	}
	return scope, true
}
