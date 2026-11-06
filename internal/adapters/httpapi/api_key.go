package httpapi

import (
	"net/http"

	contract "github.com/alibaba/open-guardrail/api/openapi"
	"github.com/alibaba/open-guardrail/internal/application/identity"
)

func (s *server) ListProjectAPIKeys(w http.ResponseWriter, r *http.Request, params contract.ListProjectAPIKeysParams) {
	if !s.guard(w, r) {
		return
	}
	size, after := 20, ""
	if params.PageSize != nil {
		size = *params.PageSize
	}
	if params.After != nil {
		after = *params.After
	}
	token, kind := credential(r)
	page, err := s.options.Identity.ListAPIKeys(r.Context(), token, kind, after, size)
	if err != nil {
		s.identityError(w, r, err)
		return
	}
	// Application metadata deliberately has no plaintext or hash fields.
	writeJSON(w, http.StatusOK, page)
}

func (s *server) RevokeProjectAPIKey(w http.ResponseWriter, r *http.Request, id contract.APIKeyID, _ contract.RevokeProjectAPIKeyParams) {
	if !s.guard(w, r) {
		return
	}
	token, kind := credential(r)
	if err := s.options.Identity.RevokeAPIKey(r.Context(), token, kind, id); err != nil {
		s.identityError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) RotateProjectAPIKey(w http.ResponseWriter, r *http.Request, id contract.APIKeyID, _ contract.RotateProjectAPIKeyParams) {
	if !s.guard(w, r) {
		return
	}
	var input contract.RotateAPIKeyRequest
	if !decodeJSON(w, r, &input, 4096, "expectedVersion") {
		s.identityError(w, r, identity.ErrInvalid)
		return
	}
	token, kind := credential(r)
	result, err := s.options.Identity.RotateAPIKey(r.Context(), token, kind, id, input.ExpectedVersion, input.ExpiresInSeconds)
	if err != nil {
		s.identityError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, contractAPIKeyResult(result))
}

func contractAPIKeyResult(result identity.APIKeyResult) contract.APIKeyResult {
	scopes := make([]contract.MachineScope, 0, len(result.Scopes))
	for _, scope := range result.Scopes {
		scopes = append(scopes, contract.MachineScope(scope))
	}
	return contract.APIKeyResult{
		ApiKey: result.APIKey, Id: result.ID, Role: contract.APIKeyResultRole(result.Role), Scopes: scopes,
		CreatedAt: result.CreatedAt, ExpiresAt: result.ExpiresAt, Version: result.Version,
	}
}
