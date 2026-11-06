package run

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	rundomain "github.com/alibaba/open-guardrail/internal/domain/run"
)

type createRepository struct {
	Repository
	input   PolicyRunCreateInput
	created bool
	err     error
}

type deferredClockRepository struct {
	Repository
	advance func()
	sampled int64
}

func (r *deferredClockRepository) SubmitSnapshot(_ context.Context, _ Scope, _ string, input SnapshotSubmitInput) (SnapshotSubmission, bool, error) {
	r.advance()
	r.sampled = input.Now()
	return SnapshotSubmission{}, true, nil
}

func (r *deferredClockRepository) RotatePolicySubmissionCredential(_ context.Context, _ Scope, _, _ string, now func() int64) (PolicyRun, error) {
	r.advance()
	r.sampled = now()
	return PolicyRun{}, nil
}

func TestSubmissionAndRotationDeferTimeSamplingToRepository(t *testing.T) {
	for _, operation := range []string{"submit", "rotate"} {
		t.Run(operation, func(t *testing.T) {
			seconds, calls := int64(1000), 0
			repository := &deferredClockRepository{advance: func() {
				if calls != 0 {
					t.Fatal("clock sampled before storage could acquire the Run lock")
				}
				seconds = 1060
			}}
			service := New(repository)
			service.now = func() time.Time { calls++; return time.Unix(seconds, 0) }
			if operation == "submit" {
				if _, _, err := service.Submit(context.Background(), Scope{}, "run_test", "retry", rundomain.SubmissionSnapshot{}); err != nil {
					t.Fatal(err)
				}
			} else if _, _, err := service.Rotate(context.Background(), Scope{}, "run_test"); err != nil {
				t.Fatal(err)
			}
			if repository.sampled != 1060 || calls != 1 {
				t.Fatalf("sampled=%d calls=%d", repository.sampled, calls)
			}
		})
	}
}

func (r *createRepository) CreatePolicyRun(_ context.Context, _ Scope, in PolicyRunCreateInput) (PolicyRun, bool, error) {
	r.input = in
	return PolicyRun{ID: "run_test"}, r.created, r.err
}
func TestCreateCanonicalIdentityAndCredentialLifetime(t *testing.T) {
	repository := &createRepository{created: true}
	service := New(repository)
	service.now = func() time.Time { return time.Unix(1000, 0) }
	service.newToken = func() string { return "ogrt_synthetic" }
	first := CreateInput{SceneKey: "recovery", ChangeSet: json.RawMessage(`{"b":2,"a":1}`), IdempotencyKey: "retry"}
	_, created, token, err := service.Create(context.Background(), Scope{}, first)
	if err != nil || !created || token != "ogrt_synthetic" || repository.input.Now != 1000 || repository.input.SubmissionTokenHash != hashString(token) {
		t.Fatalf("created=%v token=%q err=%v", created, token, err)
	}
	digest := repository.input.RequestDigest
	repository.created = false
	first.ChangeSet = json.RawMessage(`{ "a": 1, "b": 2 }`)
	_, created, token, err = service.Create(context.Background(), Scope{}, first)
	if err != nil || created || token != "" || repository.input.RequestDigest != digest {
		t.Fatal("replay reissued credential or changed canonical request identity")
	}
	revision := "rev_test"
	first.RevisionID = &revision
	_, _, _, err = service.Create(context.Background(), Scope{}, first)
	if err != nil || repository.input.CanSelectRevision || repository.input.PolicyRevisionID != revision || repository.input.RequestDigest == digest {
		t.Fatal("explicit Revision selection lost authorization or request identity")
	}
	_, _, _, err = service.Create(context.Background(), Scope{CanSelectRevision: true}, first)
	if err != nil || !repository.input.CanSelectRevision {
		t.Fatal("scoped Revision selection was not authorized")
	}
	repository.err = ErrUnavailable
	_, _, token, err = service.Create(context.Background(), Scope{}, first)
	if !errors.Is(err, ErrUnavailable) || token != "" {
		t.Fatal("failed create exposed a credential")
	}
}
