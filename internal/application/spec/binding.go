package spec

import (
	"context"

	"github.com/alibaba/open-guardrail/internal/domain/resource"
)

type Binding struct {
	Resource         resource.Reference `json:"resource"`
	PolicyID         string             `json:"policyId"`
	PolicyRevisionID *string            `json:"policyRevisionId,omitempty"`
}

type BindingPage struct {
	Items     []Binding           `json:"items"`
	NextAfter *resource.Reference `json:"nextAfter,omitempty"`
}

func (s *Service) PutBinding(ctx context.Context, scope Scope, scene string, binding Binding) (Binding, error) {
	if !binding.Resource.Valid() || binding.PolicyID == "" || (binding.PolicyRevisionID != nil && *binding.PolicyRevisionID == "") {
		return Binding{}, ErrInvalid
	}
	return s.Repository.PutBinding(ctx, scope, scene, binding)
}

func (s *Service) DeleteBinding(ctx context.Context, scope Scope, scene string, ref resource.Reference) error {
	if !ref.Valid() {
		return ErrInvalid
	}
	return s.Repository.DeleteBinding(ctx, scope, scene, ref)
}

func (s *Service) ListBindings(ctx context.Context, scope Scope, scene string, limit int, after *resource.Reference) (BindingPage, error) {
	if limit < 1 || limit > 100 || (after != nil && !after.Valid()) {
		return BindingPage{}, ErrInvalid
	}
	return s.Repository.ListBindings(ctx, scope, scene, limit, after)
}
