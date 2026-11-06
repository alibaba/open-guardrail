package spec

import (
	"context"
	"errors"
	"testing"
	"time"

	specdomain "github.com/alibaba/open-guardrail/internal/domain/spec"
)

const policyFixture = `apiVersion: guardrail.open/v1alpha1
kind: GuardrailSpec
metadata:
  name: recovery
spec:
  description: Recovery readiness
  instructions: Inspect health
  checks:
    - id: health
      displayName: Health
      description: Health check
      instructions: Inspect health
      criteria:
        pass: Healthy
        block: Unhealthy
        unknown: Unverified
      evidenceRequirements:
        - id: observation
          description: Health observation
          appliesToVerdicts: [PASS, BLOCKED, UNKNOWN]
`

type recordingRepository struct {
	Repository
	publication    PublicationInput
	initialization InitializationInput
	calls          int
	advance        func()
	acceptedAt     int64
}

func (r *recordingRepository) CommitPolicyRevision(_ context.Context, _ Scope, input PublicationInput) (PolicyRevision, bool, error) {
	r.calls++
	r.publication = input
	if r.advance != nil {
		r.advance()
	}
	r.acceptedAt = input.Now()
	return PolicyRevision{ID: "revision"}, true, nil
}

func (r *recordingRepository) CommitSceneInitialization(_ context.Context, _ Scope, input InitializationInput) (InitializedScene, bool, error) {
	r.calls++
	r.initialization = input
	if r.advance != nil {
		r.advance()
	}
	r.acceptedAt = input.Now()
	return InitializedScene{}, true, nil
}

func TestSpecCommandsRejectInvalidDocumentsBeforePersistence(t *testing.T) {
	repository := &recordingRepository{}
	service := New(repository)
	var validation *specdomain.ValidationError
	if _, _, err := service.PublishPolicyRevision(context.Background(), Scope{}, "policy", "invalid: [", "key", false); !errors.As(err, &validation) {
		t.Fatalf("publication error=%v", err)
	}
	if _, _, err := service.InitializeScene(context.Background(), Scope{}, "recovery", "Recovery", "Default", "invalid: [", "key"); !errors.As(err, &validation) {
		t.Fatalf("initialization error=%v", err)
	}
	if repository.calls != 0 {
		t.Fatal("invalid document reached persistence")
	}
}

func TestSpecCommandIdentityAndDeferredTime(t *testing.T) {
	ctx := context.Background()
	repository := &recordingRepository{}
	service := New(repository)
	seconds := int64(1000)
	service.now = func() time.Time { return time.Unix(seconds, 0) }
	repository.advance = func() { seconds = 1005 }
	if _, _, err := service.PublishPolicyRevision(ctx, Scope{}, "policy", policyFixture, "key", false); err != nil {
		t.Fatal(err)
	}
	first := repository.publication
	if first.CanonicalJSON == "" || first.SpecDigest == "" || first.RequestDigest == "" || repository.acceptedAt != 1005 {
		t.Fatal("publication lost validated content or sampled time before persistence")
	}
	// Publication is bound to the exact reviewed YAML; initialization is bound
	// to semantic Spec identity. A comment changes only the former retry identity.
	commented := "# same semantic policy\n" + policyFixture
	if _, _, err := service.PublishPolicyRevision(ctx, Scope{}, "policy", commented, "key", false); err != nil {
		t.Fatal(err)
	}
	if repository.publication.SpecDigest != first.SpecDigest || repository.publication.RequestDigest == first.RequestDigest {
		t.Fatal("publication did not preserve reviewed-content identity")
	}
	if _, _, err := service.PublishPolicyRevision(ctx, Scope{}, "policy", policyFixture, "key", true); err != nil {
		t.Fatal(err)
	}
	if !repository.publication.FromDraft || repository.publication.OperationType == first.OperationType || repository.publication.RequestDigest == first.RequestDigest {
		t.Fatal("Draft publication reused direct-publication retry identity")
	}
	if _, _, err := service.InitializeScene(ctx, Scope{}, "recovery", "Recovery", "Default", policyFixture, "init"); err != nil {
		t.Fatal(err)
	}
	initial := repository.initialization
	if _, _, err := service.InitializeScene(ctx, Scope{}, "recovery", "Recovery", "Default", commented, "init"); err != nil {
		t.Fatal(err)
	}
	if initial.RequestDigest != repository.initialization.RequestDigest || initial.SpecDigest != repository.initialization.SpecDigest {
		t.Fatal("initialization retry identity changed for a YAML comment")
	}
}
