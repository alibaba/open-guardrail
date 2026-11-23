package spec

import (
	"context"
	"strings"

	"github.com/alibaba/open-guardrail/extension/resource"
)

// Repository writes enforce the persisted maintainer list in the same transaction.
func (s *Service) GetSceneModel(ctx context.Context, scope Scope, key string) (SceneModel, error) {
	return s.repository.GetSceneModel(ctx, scope, key)
}
func (s *Service) ListSceneModels(ctx context.Context, scope Scope) ([]SceneModel, error) {
	return s.repository.ListSceneModels(ctx, scope)
}
func (s *Service) GetPolicy(ctx context.Context, scope Scope, id string) (Policy, error) {
	if err := s.checkPolicyRead(ctx, scope, id); err != nil {
		return Policy{}, err
	}
	return s.repository.GetPolicy(ctx, scope, id)
}
func (s *Service) GetPolicyRevision(ctx context.Context, scope Scope, id string) (PolicyRevision, error) {
	revision, err := s.repository.GetPolicyRevision(ctx, scope, id)
	if err != nil {
		return PolicyRevision{}, err
	}
	if err := s.checkPolicyRead(ctx, scope, revision.PolicyID); err != nil {
		return PolicyRevision{}, err
	}
	return revision, nil
}
func (s *Service) ListPolicies(ctx context.Context, scope Scope, sceneKey string) ([]Policy, error) {
	return s.repository.ListPolicies(ctx, scope, sceneKey)
}
func (s *Service) ListPolicyRevisions(ctx context.Context, scope Scope, policyID string, pageSize int, before int64) (RevisionPage, error) {
	if err := s.checkPolicyRead(ctx, scope, policyID); err != nil {
		return RevisionPage{}, err
	}
	return s.repository.ListPolicyRevisions(ctx, scope, policyID, pageSize, before)
}
func (s *Service) CreatePolicy(ctx context.Context, scope Scope, sceneKey, name, specKey, key string, refs ...resource.Reference) (Policy, bool, error) {
	if len(refs) > 0 && (len(refs) != 1 || !refs[0].Valid()) {
		return Policy{}, false, ErrInvalid
	}
	return s.repository.CreatePolicy(ctx, scope, sceneKey, name, specKey, key, refs...)
}
func (s *Service) GetPolicyDraft(ctx context.Context, scope Scope, id string) (PolicyRevision, error) {
	if err := s.checkPolicyRead(ctx, scope, id); err != nil {
		return PolicyRevision{}, err
	}
	return s.repository.GetPolicyDraft(ctx, scope, id)
}
func (s *Service) SavePolicyDraft(ctx context.Context, scope Scope, id, yaml string) (PolicyRevision, error) {
	return s.repository.SavePolicyDraft(ctx, scope, id, yaml)
}
func (s *Service) ActivatePolicyRevision(ctx context.Context, scope Scope, policyID, revisionID string, expected *string) (Policy, error) {
	return s.repository.ActivatePolicyRevision(ctx, scope, policyID, revisionID, expected)
}
func (s *Service) SetDefaultPolicy(ctx context.Context, scope Scope, sceneKey, policyID string, expected *string) (SceneModel, error) {
	return s.repository.SetDefaultPolicy(ctx, scope, sceneKey, policyID, expected)
}
func (s *Service) SetSceneTTL(ctx context.Context, scope Scope, sceneKey string, ttl int64) (SceneModel, error) {
	return s.UpdateScene(ctx, scope, sceneKey, &ttl, nil)
}

func (s *Service) UpdateScene(ctx context.Context, scope Scope, sceneKey string, ttl *int64, plugins *resource.Plugins) (SceneModel, error) {
	if (ttl == nil && plugins == nil) || !plugins.Valid() || (ttl != nil && (*ttl < 1 || *ttl > 604800)) {
		return SceneModel{}, ErrInvalid
	}
	return s.repository.UpdateScene(ctx, scope, sceneKey, ttl, plugins)
}
func (s *Service) SetPolicyArchived(ctx context.Context, scope Scope, id string, archived bool) (Policy, error) {
	return s.repository.SetPolicyArchived(ctx, scope, id, archived)
}
func (s *Service) checkPolicyRead(ctx context.Context, scope Scope, policyID string) error {
	if !strings.HasPrefix(scope.ActorID, "key:") {
		return nil
	}
	p, err := s.repository.GetPolicy(ctx, scope, policyID)
	if err != nil {
		return err
	}
	if !p.CanMaintain(scope.ActorID) {
		return ErrForbidden
	}
	return nil
}
