package httpapi

import (
	"github.com/alibaba/open-guardrail/internal/application/identity"
	runapp "github.com/alibaba/open-guardrail/internal/application/run"
	"github.com/alibaba/open-guardrail/internal/application/spec"
	"net/http"
)

func (s *server) specScope(w http.ResponseWriter, r *http.Request) (spec.Scope, bool) {
	if !s.guard(w, r) || s.options.ModelSpecs == nil {
		return spec.Scope{}, false
	}
	token, kind := credential(r)
	if kind != "browser" && kind != "cli" {
		s.identityError(w, r, identity.ErrUnauthorized)
		return spec.Scope{}, false
	}
	principal, err := s.options.Identity.Authenticate(r.Context(), token, kind)
	if err != nil {
		s.identityError(w, r, err)
		return spec.Scope{}, false
	}
	if principal.Role != "ADMIN" || principal.User == nil {
		s.identityError(w, r, identity.ErrUnauthorized)
		return spec.Scope{}, false
	}
	return spec.Scope{
		WorkspaceID: principal.WorkspaceID,
		ProjectID:   principal.ProjectID,
		ActorID:     principal.User.ID,
	}, true
}

// policyPublisherScope permits Policy creation and direct Revision publication.
// A POLICY_PUBLISHER key cannot edit drafts, activate revisions or initialize Scenes.
func (s *server) policyPublisherScope(w http.ResponseWriter, r *http.Request, requiredScope string) (spec.Scope, bool) {
	token, kind := credential(r)
	if kind != "key" {
		return s.specScope(w, r)
	}
	if !s.guard(w, r) || s.options.ModelSpecs == nil {
		return spec.Scope{}, false
	}
	principal, err := s.options.Identity.Authenticate(r.Context(), token, kind)
	if err != nil {
		s.identityError(w, r, err)
		return spec.Scope{}, false
	}
	if !principal.Allows(requiredScope) {
		writeError(w, r, http.StatusForbidden, "PERMISSION_DENIED", "Machine key lacks the required scope")
		return spec.Scope{}, false
	}
	return spec.Scope{WorkspaceID: principal.WorkspaceID, ProjectID: principal.ProjectID,
		ActorID: "key:" + principal.CredentialID}, true
}

func (s *server) runScope(w http.ResponseWriter, r *http.Request, requiredScope string) (runapp.Scope, bool) {
	if s.options.Identity == nil || s.options.ModelRuns == nil {
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
		return runapp.Scope{}, false
	}
	token, kind := credential(r)
	if kind != "key" {
		s.identityError(w, r, identity.ErrUnauthorized)
		return runapp.Scope{}, false
	}
	principal, err := s.options.Identity.Authenticate(r.Context(), token, kind)
	if err != nil {
		s.identityError(w, r, err)
		return runapp.Scope{}, false
	}
	if !principal.Allows(requiredScope) {
		writeError(w, r, http.StatusForbidden, "PERMISSION_DENIED", "Machine key lacks the required scope")
		return runapp.Scope{}, false
	}
	return runapp.Scope{
		WorkspaceID:       principal.WorkspaceID,
		ProjectID:         principal.ProjectID,
		ActorID:           "key:" + principal.CredentialID,
		CanSelectRevision: principal.Allows(identity.ScopeRunsCreateWithRevision),
	}, true
}

func (s *server) managedRunScope(w http.ResponseWriter, r *http.Request) (runapp.Scope, bool) {
	if !s.guard(w, r) || s.options.ModelRuns == nil {
		return runapp.Scope{}, false
	}
	token, kind := credential(r)
	if kind != "browser" && kind != "cli" {
		s.identityError(w, r, identity.ErrUnauthorized)
		return runapp.Scope{}, false
	}
	principal, err := s.options.Identity.Authenticate(r.Context(), token, kind)
	if err != nil {
		s.identityError(w, r, err)
		return runapp.Scope{}, false
	}
	if principal.Role != "ADMIN" {
		writeError(w, r, http.StatusForbidden, "PERMISSION_DENIED", "Administrator role required")
		return runapp.Scope{}, false
	}
	if principal.User == nil {
		s.identityError(w, r, identity.ErrUnauthorized)
		return runapp.Scope{}, false
	}
	return runapp.Scope{
		WorkspaceID: principal.WorkspaceID,
		ProjectID:   principal.ProjectID,
		ActorID:     principal.User.ID,
	}, true
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
