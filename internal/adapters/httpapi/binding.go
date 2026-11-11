package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"net/http"

	contract "github.com/alibaba/open-guardrail/api/openapi"
	"github.com/alibaba/open-guardrail/internal/application/spec"
	"github.com/alibaba/open-guardrail/internal/domain/resource"
)

func (s *server) ListBindings(w http.ResponseWriter, r *http.Request, key string, params contract.ListBindingsParams) {
	scope, ok := s.specScope(w, r)
	if !ok {
		return
	}
	if !modelSceneKey.MatchString(key) {
		s.modelSpecError(w, r, spec.ErrInvalid)
		return
	}
	limit := 20
	if params.Limit != nil {
		limit = *params.Limit
	}
	var after *resource.Reference
	if params.After != nil {
		data, err := base64.RawURLEncoding.DecodeString(*params.After)
		var ref resource.Reference
		if err != nil || len(data) > 1024 || json.Unmarshal(data, &ref) != nil || !ref.Valid() {
			s.modelSpecError(w, r, spec.ErrInvalid)
			return
		}
		after = &ref
	}
	page, err := s.options.ModelSpecs.ListBindings(r.Context(), scope, key, limit, after)
	if err != nil {
		s.modelSpecError(w, r, err)
		return
	}
	response := struct {
		Items     []spec.Binding `json:"items"`
		NextAfter *string        `json:"nextAfter,omitempty"`
	}{Items: page.Items}
	if page.NextAfter != nil {
		data, _ := json.Marshal(page.NextAfter)
		cursor := base64.RawURLEncoding.EncodeToString(data)
		response.NextAfter = &cursor
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *server) PutBinding(w http.ResponseWriter, r *http.Request, key, kind, resourceId string, params contract.PutBindingParams) {
	scope, ok := s.specScope(w, r)
	if !ok {
		return
	}
	ref := resource.Reference{Kind: kind, ID: resourceId}
	if !modelSceneKey.MatchString(key) || !ref.Valid() {
		s.modelSpecError(w, r, spec.ErrInvalid)
		return
	}
	var body contract.PutBindingRequest
	if !s.decodeModel(w, r, &body, 4096, "policyId") {
		return
	}
	if !modelID.MatchString(body.PolicyId) || (body.PolicyRevisionId != nil && !modelID.MatchString(*body.PolicyRevisionId)) {
		s.modelSpecError(w, r, spec.ErrInvalid)
		return
	}
	binding, err := s.options.ModelSpecs.PutBinding(r.Context(), scope, key, spec.Binding{Resource: ref, PolicyID: body.PolicyId, PolicyRevisionID: body.PolicyRevisionId})
	if err != nil {
		s.modelSpecError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, binding)
}

func (s *server) DeleteBinding(w http.ResponseWriter, r *http.Request, key, kind, resourceId string, params contract.DeleteBindingParams) {
	scope, ok := s.specScope(w, r)
	if !ok {
		return
	}
	if !modelSceneKey.MatchString(key) {
		s.modelSpecError(w, r, spec.ErrInvalid)
		return
	}
	if err := s.options.ModelSpecs.DeleteBinding(r.Context(), scope, key, resource.Reference{Kind: kind, ID: resourceId}); err != nil {
		s.modelSpecError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
