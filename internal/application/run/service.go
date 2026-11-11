package run

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"time"

	"github.com/alibaba/open-guardrail/internal/application/identity"
	"github.com/alibaba/open-guardrail/internal/domain/resource"
	rundomain "github.com/alibaba/open-guardrail/internal/domain/run"
)

// Service owns Run orchestration; transports only validate and map their contract.
type Service struct {
	Repository
	now      func() time.Time
	newToken func() string
}

func New(repository Repository) *Service {
	return &Service{Repository: repository, now: time.Now, newToken: func() string { return identity.RandomToken("ogrt_") }}
}

type CreateInput struct {
	Resources      []resource.Reference
	SceneKey       string
	ChangeSet      json.RawMessage
	RevisionID     *string
	IdempotencyKey string
}

func (s *Service) Create(ctx context.Context, scope Scope, input CreateInput) (PolicyRun, bool, string, error) {
	canonical, digest, err := rundomain.Canonicalize(input.ChangeSet)
	if err != nil {
		return PolicyRun{}, false, "", ErrInvalid
	}
	revisionID, mode := "", "default"
	if input.RevisionID != nil {
		revisionID, mode = *input.RevisionID, "revision"
	}
	requestDigest := hashString(input.SceneKey + "\n" + string(canonical) + "\n" + mode + "\n" + revisionID)
	var refs []resource.Reference
	if len(input.Resources) > 0 {
		if input.RevisionID != nil {
			return PolicyRun{}, false, "", ErrInvalid
		}
		refs = slices.Clone(input.Resources)
		slices.SortFunc(refs, func(a, b resource.Reference) int {
			if n := cmp.Compare(a.Kind, b.Kind); n != 0 {
				return n
			}
			return cmp.Compare(a.ID, b.ID)
		})
		if !resource.ValidReferences(refs) {
			return PolicyRun{}, false, "", ErrInvalid
		}
		encoded, _ := json.Marshal(refs)
		requestDigest = hashString(requestDigest + "\n" + string(encoded))
	}
	token := s.newToken()
	run, created, err := s.Repository.CreatePolicyRun(ctx, scope, PolicyRunCreateInput{
		SceneKey:            input.SceneKey,
		PolicyRevisionID:    revisionID,
		CanSelectRevision:   scope.CanSelectRevision,
		Resources:           refs,
		ChangeSetJSON:       string(canonical),
		ChangeSetDigest:     digest,
		IdempotencyKey:      input.IdempotencyKey,
		RequestDigest:       requestDigest,
		SubmissionTokenHash: hashString(token),
		Now:                 s.now().Unix(),
	})
	if err != nil || !created {
		return run, created, "", err
	}
	return run, true, token, nil
}
func (s *Service) Submit(ctx context.Context, scope Scope, id, key string, snapshot rundomain.SubmissionSnapshot) (SnapshotSubmission, bool, error) {
	return s.Repository.SubmitSnapshot(ctx, scope, id, SnapshotSubmitInput{IdempotencyKey: key, Snapshot: snapshot, Now: s.nowUnix})
}
func (s *Service) Rotate(ctx context.Context, scope Scope, id string) (PolicyRun, string, error) {
	token := s.newToken()
	run, err := s.Repository.RotatePolicySubmissionCredential(ctx, scope, id, hashString(token), s.nowUnix)
	if err != nil {
		return PolicyRun{}, "", err
	}
	return run, token, nil
}
func (s *Service) Get(ctx context.Context, scope Scope, id string) (PolicyRun, error) {
	return s.Repository.RefreshPolicyRun(ctx, scope, id, s.now().Unix())
}
func (s *Service) List(ctx context.Context, scope Scope, scene, status string, limit int, cursorAt int64, cursorID string) ([]PolicyRunSummary, bool, error) {
	return s.Repository.ListPolicyRuns(ctx, scope, scene, status, limit, cursorAt, cursorID, s.now().Unix())
}
func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (s *Service) nowUnix() int64 { return s.now().Unix() }

func (s *Service) Authenticate(ctx context.Context, id, token string) (Scope, error) {
	return s.Repository.AuthenticateSubmissionToken(ctx, id, hashString(token))
}
