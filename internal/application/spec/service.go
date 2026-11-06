package spec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	specdomain "github.com/alibaba/open-guardrail/internal/domain/spec"
)

// Service prepares publication commands. Repositories retain the transactions,
// lock ordering and state comparisons needed to commit each command atomically.
type Service struct {
	Repository
	now func() time.Time
}

func New(repository Repository) *Service { return &Service{Repository: repository, now: time.Now} }

type PublicationInput struct {
	PolicyID, YAML, IdempotencyKey           string
	CanonicalJSON, SpecDigest, RequestDigest string
	OperationType                            string
	FromDraft                                bool
	Now                                      func() int64
}

type InitializationInput struct {
	SceneKey, SceneName, PolicyName, YAML, IdempotencyKey string
	CanonicalJSON, SpecDigest, RequestDigest              string
	Now                                                   func() int64
}

func (s *Service) PublishPolicyRevision(ctx context.Context, scope Scope, policyID, yaml, key string, fromDraft bool) (PolicyRevision, bool, error) {
	canonical, digest, err := specdomain.Validate(yaml)
	if err != nil {
		return PolicyRevision{}, false, err
	}
	operation, request := "revision:direct", policyID+"\n"+yaml
	if fromDraft {
		operation, request = "revision:draft", policyID+"\ndraft\n"+yaml
	}
	return s.Repository.CommitPolicyRevision(ctx, scope, PublicationInput{
		PolicyID: policyID, YAML: yaml, IdempotencyKey: key, FromDraft: fromDraft,
		CanonicalJSON: canonical, SpecDigest: digest, RequestDigest: requestDigest(request),
		OperationType: operation, Now: s.nowUnix,
	})
}

func (s *Service) InitializeScene(ctx context.Context, scope Scope, sceneKey, sceneName, policyName, yaml, key string) (InitializedScene, bool, error) {
	canonical, digest, err := specdomain.Validate(yaml)
	if err != nil {
		return InitializedScene{}, false, err
	}
	return s.Repository.CommitSceneInitialization(ctx, scope, InitializationInput{
		SceneKey: sceneKey, SceneName: sceneName, PolicyName: policyName, YAML: yaml, IdempotencyKey: key,
		CanonicalJSON: canonical, SpecDigest: digest,
		RequestDigest: requestDigest(sceneKey + "\n" + sceneName + "\n" + policyName + "\n" + canonical), Now: s.nowUnix,
	})
}

func (s *Service) nowUnix() int64 { return s.now().Unix() }

func requestDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
