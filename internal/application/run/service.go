package run

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/alibaba/open-guardrail/extension/resource"
	opaque "github.com/alibaba/open-guardrail/internal/application/token"
	rundomain "github.com/alibaba/open-guardrail/internal/domain/run"
)

// Service owns Run orchestration; transports only validate and map their contract.
type Service struct {
	repository   Repository
	scenePlugins func(context.Context, Scope, string) (ScenePlugins, error)
	now          func() time.Time
	newToken     func() string
}

func New(repository Repository) *Service {
	return &Service{repository: repository, now: time.Now, newToken: func() string { return opaque.RandomToken("ogrt_") }}
}

type ScenePlugins struct {
	Enricher resource.ChangeSetEnricher
	Resolver resource.Resolver
}

func (s *Service) WithScenePlugins(lookup func(context.Context, Scope, string) (ScenePlugins, error)) *Service {
	s.scenePlugins = lookup
	return s
}

type CreateInput struct {
	Resources      []resource.Reference
	SceneKey       string
	BusinessID     string
	ChangeSet      json.RawMessage
	RevisionID     *string
	IdempotencyKey string
}

func (s *Service) Create(ctx context.Context, scope Scope, input CreateInput) (PolicyRun, bool, string, error) {
	if input.RevisionID != nil && !scope.CanSelectRevision {
		return PolicyRun{}, false, "", ErrUnauthorized
	}
	canonical, _, err := rundomain.Canonicalize(input.ChangeSet)
	if err != nil {
		return PolicyRun{}, false, "", ErrInvalid
	}
	revisionID, mode := "", "default"
	if input.RevisionID != nil {
		revisionID, mode = *input.RevisionID, "revision"
	}
	requestIdentity := input.SceneKey + "\n" + string(canonical) + "\n" + mode + "\n" + revisionID
	if input.BusinessID != "" {
		requestIdentity += "\nbusinessId:" + input.BusinessID
	}
	requestDigest := hashString(requestIdentity)
	groups := [][]resource.Reference{}
	if len(input.Resources) > 0 {
		if input.RevisionID != nil {
			return PolicyRun{}, false, "", ErrInvalid
		}
		refs := slices.Clone(input.Resources)
		slices.SortFunc(refs, func(a, b resource.Reference) int {
			if n := cmp.Compare(a.Kind, b.Kind); n != 0 {
				return n
			}
			return cmp.Compare(a.ID, b.ID)
		})
		groups = append(groups, refs)
		if !(resource.Resolution{Groups: groups}).Valid() {
			return PolicyRun{}, false, "", ErrInvalid
		}
		encoded, _ := json.Marshal(refs)
		requestDigest = hashString(requestDigest + "\n" + string(encoded))
	}
	var plugins ScenePlugins
	if s.scenePlugins != nil {
		// Exact retries never re-query mutable external business state.
		existing, err := s.repository.ReplayPolicyRun(ctx, scope, input.IdempotencyKey, input.SceneKey, input.BusinessID, requestDigest, s.now().Unix())
		if err == nil {
			return existing, false, "", nil
		}
		if !errors.Is(err, ErrNotFound) {
			return PolicyRun{}, false, "", err
		}
		plugins, err = s.scenePlugins(ctx, scope, input.SceneKey)
		if err != nil {
			return PolicyRun{}, false, "", err
		}
	}
	if plugins.Enricher != nil {
		enriched, err := plugins.Enricher.EnrichChangeSet(ctx, resource.ChangeSetRequest{SceneKey: input.SceneKey, ChangeSet: canonical})
		if errors.Is(err, resource.ErrInvalid) {
			return PolicyRun{}, false, "", ErrInvalid
		}
		if err != nil {
			return PolicyRun{}, false, "", resource.ErrUnavailable
		}
		canonical, _, err = rundomain.Canonicalize(enriched)
		if err != nil {
			return PolicyRun{}, false, "", resource.ErrUnavailable
		}
	}
	_, digest, _ := rundomain.Canonicalize(canonical)
	if plugins.Resolver != nil && input.RevisionID == nil && len(input.Resources) == 0 {
		resolved, err := plugins.Resolver.ResolveResources(ctx, resource.ResolveRequest{
			SceneKey: input.SceneKey, ChangeSet: canonical,
		})
		if errors.Is(err, resource.ErrInvalid) {
			return PolicyRun{}, false, "", ErrInvalid
		}
		if err != nil || !resolved.Valid() {
			return PolicyRun{}, false, "", resource.ErrUnavailable
		}
		groups = resolved.Groups
	}
	token := s.newToken()
	run, created, err := s.repository.CreatePolicyRun(ctx, scope, PolicyRunCreateInput{
		SceneKey: input.SceneKey, BusinessID: input.BusinessID, PolicyRevisionID: revisionID, CanSelectRevision: scope.CanSelectRevision,
		ResourceGroups: groups, ChangeSetJSON: string(canonical), ChangeSetDigest: digest,
		IdempotencyKey: input.IdempotencyKey, RequestDigest: requestDigest,
		SubmissionTokenHash: hashString(token), Now: s.now().Unix(),
	})
	if err != nil || !created {
		return run, created, "", err
	}
	return run, true, token, nil
}
func (s *Service) Submit(ctx context.Context, scope Scope, id, key string, snapshot rundomain.SubmissionSnapshot) (SnapshotSubmission, bool, error) {
	return s.repository.SubmitSnapshot(ctx, scope, id, SnapshotSubmitInput{IdempotencyKey: key, Snapshot: snapshot, Now: s.nowUnix})
}
func (s *Service) Rotate(ctx context.Context, scope Scope, id string) (PolicyRun, string, error) {
	token := s.newToken()
	run, err := s.repository.RotatePolicySubmissionCredential(ctx, scope, id, hashString(token), s.nowUnix)
	if err != nil {
		return PolicyRun{}, "", err
	}
	return run, token, nil
}
func (s *Service) Get(ctx context.Context, scope Scope, id string) (PolicyRun, error) {
	if scope.SubmissionCredentialHash != "" && scope.ActorID != "run-token:"+id {
		return PolicyRun{}, ErrUnauthorized
	}
	return s.repository.RefreshPolicyRun(ctx, scope, id, s.now().Unix())
}
func (s *Service) List(ctx context.Context, scope Scope, scene, businessID, status string, limit int, cursorAt int64, cursorID string) ([]PolicyRunSummary, bool, error) {
	if scope.SubmissionCredentialHash != "" {
		return nil, false, ErrUnauthorized
	}
	return s.repository.ListPolicyRuns(ctx, scope, scene, businessID, status, limit, cursorAt, cursorID, s.now().Unix())
}
func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (s *Service) nowUnix() int64 { return s.now().Unix() }

func (s *Service) Authenticate(ctx context.Context, id, token string) (Scope, error) {
	return s.repository.AuthenticateSubmissionToken(ctx, id, hashString(token))
}
