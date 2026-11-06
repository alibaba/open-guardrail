package spec

import "context"

// Repository defines the persistence boundary independently of the SQL adapter.
type Repository interface {
	GetSceneModel(ctx context.Context, scope Scope, key string) (SceneModel, error)
	ListSceneModels(ctx context.Context, scope Scope) ([]SceneModel, error)
	GetPolicy(ctx context.Context, scope Scope, id string) (Policy, error)
	GetPolicyRevision(ctx context.Context, scope Scope, id string) (PolicyRevision, error)
	ListPolicies(ctx context.Context, scope Scope, sceneKey string) ([]Policy, error)
	ListPolicyRevisions(ctx context.Context, scope Scope, policyID string, pageSize int, before int64) (RevisionPage, error)
	CreatePolicy(ctx context.Context, scope Scope, sceneKey, name, key string) (Policy, bool, error)
	GetPolicyDraft(ctx context.Context, scope Scope, id string) (PolicyRevision, error)
	SavePolicyDraft(ctx context.Context, scope Scope, id, yaml string) (PolicyRevision, error)
	// CommitPolicyRevision rechecks the reviewed Draft under its write lock.
	CommitPolicyRevision(ctx context.Context, scope Scope, input PublicationInput) (PolicyRevision, bool, error)
	ActivatePolicyRevision(ctx context.Context, scope Scope, policyID, revisionID string, expected *string) (Policy, error)
	SetDefaultPolicy(ctx context.Context, scope Scope, sceneKey, policyID string, expected *string) (SceneModel, error)
	SetSceneTTL(ctx context.Context, scope Scope, sceneKey string, ttl int64) (SceneModel, error)
	SetPolicyArchived(ctx context.Context, scope Scope, id string, archived bool) (Policy, error)
	CommitSceneInitialization(ctx context.Context, scope Scope, input InitializationInput) (InitializedScene, bool, error)
}
