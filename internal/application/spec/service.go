package spec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/alibaba/open-guardrail/extension/resource"
	rundomain "github.com/alibaba/open-guardrail/internal/domain/run"
	specdomain "github.com/alibaba/open-guardrail/internal/domain/spec"
)

// Service prepares publication commands. Repositories retain the transactions,
// lock ordering and state comparisons needed to commit each command atomically.
type Service struct {
	repository Repository
	now        func() time.Time
}

func New(repository Repository) *Service {
	return &Service{repository: repository, now: time.Now}
}

type PublicationInput struct {
	PolicyID, YAML, IdempotencyKey           string
	CanonicalJSON, SpecDigest, RequestDigest string
	OperationType                            string
	FromDraft                                bool
	Metadata                                 json.RawMessage
	Now                                      func() int64
}

type InitializationInput struct {
	Plugins                                               *resource.Plugins
	SceneKey, SceneName, PolicyName, YAML, IdempotencyKey string
	CanonicalJSON, SpecDigest, RequestDigest              string
	Now                                                   func() int64
}

type PublicationOptions struct {
	Metadata json.RawMessage
}

func (s *Service) PublishPolicyRevision(ctx context.Context, scope Scope, policyID, yaml, key string, fromDraft bool) (PolicyRevision, bool, error) {
	return s.PublishPolicyRevisionWithOptions(ctx, scope, policyID, yaml, key, fromDraft, PublicationOptions{})
}

func (s *Service) PublishPolicyRevisionWithOptions(ctx context.Context, scope Scope, policyID, yaml, key string, fromDraft bool, options PublicationOptions) (PolicyRevision, bool, error) {
	canonical, digest, err := specdomain.Validate(yaml)
	if err != nil {
		return PolicyRevision{}, false, err
	}
	var meta json.RawMessage
	if len(options.Metadata) > 0 {
		if len(options.Metadata) > 64<<10 {
			return PolicyRevision{}, false, ErrInvalid
		}
		meta, _, err = rundomain.Canonicalize(options.Metadata)
		if err != nil || len(meta) > 64<<10 {
			return PolicyRevision{}, false, ErrInvalid
		}
	}
	operation, request := "revision:direct", policyID+"\n"+yaml
	if fromDraft {
		operation, request = "revision:draft", policyID+"\ndraft\n"+yaml
	}
	if len(meta) > 0 {
		request += "\nmetadata\n" + string(meta)
	}
	return s.repository.CommitPolicyRevision(ctx, scope, PublicationInput{
		PolicyID: policyID, YAML: yaml, IdempotencyKey: key, FromDraft: fromDraft,
		CanonicalJSON: canonical, SpecDigest: digest, RequestDigest: requestDigest(request),
		OperationType: operation, Now: s.nowUnix,
		Metadata: meta,
	})
}

func (s *Service) InitializeScene(ctx context.Context, scope Scope, sceneKey, sceneName, policyName, yaml, key string) (InitializedScene, bool, error) {
	return s.InitializeSceneWithPlugins(ctx, scope, sceneKey, sceneName, policyName, yaml, key, nil)
}

func (s *Service) InitializeSceneWithPlugins(ctx context.Context, scope Scope, sceneKey, sceneName, policyName, yaml, key string, plugins *resource.Plugins) (InitializedScene, bool, error) {
	if !plugins.Valid() {
		return InitializedScene{}, false, ErrInvalid
	}
	canonical, digest, err := specdomain.Validate(yaml)
	if err != nil {
		return InitializedScene{}, false, err
	}
	request := sceneKey + "\n" + sceneName + "\n" + policyName + "\n" + canonical
	if plugins != nil {
		encoded, _ := json.Marshal(plugins)
		request += "\nplugins\n" + string(encoded)
	}
	return s.repository.CommitSceneInitialization(ctx, scope, InitializationInput{
		SceneKey: sceneKey, SceneName: sceneName, PolicyName: policyName, YAML: yaml, IdempotencyKey: key,
		CanonicalJSON: canonical, SpecDigest: digest,
		Plugins: plugins, RequestDigest: requestDigest(request), Now: s.nowUnix,
	})
}

func (s *Service) nowUnix() int64 { return s.now().Unix() }

func requestDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
