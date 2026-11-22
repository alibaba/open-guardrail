package run

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/alibaba/open-guardrail/extension/resource"
)

type resolverFunc func(context.Context, resource.ResolveRequest) (resource.Resolution, error)

type enricherFunc func(context.Context, resource.ChangeSetRequest) (json.RawMessage, error)

func (f enricherFunc) EnrichChangeSet(ctx context.Context, r resource.ChangeSetRequest) (json.RawMessage, error) {
	return f(ctx, r)
}

func (f resolverFunc) ResolveResources(ctx context.Context, r resource.ResolveRequest) (resource.Resolution, error) {
	return f(ctx, r)
}

type replayRepository struct {
	createRepository
	replay    PolicyRun
	replayErr error
}

func (r *replayRepository) ReplayPolicyRun(context.Context, Scope, string, string, string, string, int64) (PolicyRun, error) {
	return r.replay, r.replayErr
}

func TestResourceResolverIsOutsidePersistenceAndSkippedForExactRetry(t *testing.T) {
	repo := &replayRepository{replayErr: ErrNotFound}
	calls := 0
	service := New(repo).WithScenePlugins(fixedScenePlugins(nil, resolverFunc(func(_ context.Context, r resource.ResolveRequest) (resource.Resolution, error) {
		calls++
		if r.SceneKey != "recovery" {
			t.Fatal("lost scene context")
		}
		return resource.Resolution{Groups: [][]resource.Reference{{{Kind: "application", ID: "7"}}}}, nil
	})))
	input := CreateInput{SceneKey: "recovery", ChangeSet: json.RawMessage(`{}`), IdempotencyKey: "same"}
	scope := Scope{WorkspaceID: "w", ProjectID: "p", Operator: "100001"}
	if _, _, _, err := service.Create(context.Background(), scope, input); err != nil || calls != 1 || len(repo.input.ResourceGroups) != 1 {
		t.Fatalf("resolution: calls=%d err=%v", calls, err)
	}
	repo.replay, repo.replayErr = PolicyRun{ID: "frozen"}, nil
	if run, created, token, err := service.Create(context.Background(), scope, input); err != nil || run.ID != "frozen" || created || token != "" || calls != 1 {
		t.Fatal("exact retry called resolver or issued credential")
	}
	input.Resources = []resource.Reference{{Kind: "application", ID: "forged"}}
	repo.replayErr = ErrNotFound
	if _, _, _, err := service.Create(context.Background(), scope, input); err != nil || calls != 1 || repo.input.ResourceGroups[0][0].ID != "forged" {
		t.Fatal("explicit resources should bypass the candidate provider")
	}
	input.Resources = nil
	repo.replayErr = ErrNotFound
	repo.input = PolicyRunCreateInput{}
	service.WithScenePlugins(fixedScenePlugins(nil, resolverFunc(func(context.Context, resource.ResolveRequest) (resource.Resolution, error) {
		return resource.Resolution{}, errors.New("offline")
	})))
	if _, _, _, err := service.Create(context.Background(), scope, input); !errors.Is(err, resource.ErrUnavailable) || repo.input.SceneKey != "" {
		t.Fatal("resolver failure reached storage or fell back")
	}
}

func TestInvalidBusinessReferenceNeverReachesRunPersistence(t *testing.T) {
	repo := &replayRepository{replayErr: ErrNotFound}
	service := New(repo).WithScenePlugins(fixedScenePlugins(nil, resolverFunc(func(context.Context, resource.ResolveRequest) (resource.Resolution, error) {
		return resource.Resolution{}, resource.ErrInvalid
	})))
	_, _, _, err := service.Create(context.Background(), Scope{}, CreateInput{SceneKey: "recovery", ChangeSet: json.RawMessage(`{}`), IdempotencyKey: "invalid"})
	if !errors.Is(err, ErrInvalid) || repo.input.SceneKey != "" {
		t.Fatal("invalid reference was treated as dependency failure or persisted")
	}
}

func TestChangeSetIsEnrichedBeforeCandidatesAndFrozen(t *testing.T) {
	repo := &replayRepository{replayErr: ErrNotFound}
	calls := 0
	service := New(repo).WithScenePlugins(fixedScenePlugins(enricherFunc(func(_ context.Context, request resource.ChangeSetRequest) (json.RawMessage, error) {
		calls++
		if request.SceneKey != "recovery" || string(request.ChangeSet) != `{"flowInstId":"123"}` {
			t.Fatal("enricher did not receive raw canonical changeSet")
		}
		return json.RawMessage(`{"flowInstId":"123","commonContext":{"appId":7}}`), nil
	}), resolverFunc(func(_ context.Context, request resource.ResolveRequest) (resource.Resolution, error) {
		if string(request.ChangeSet) != `{"commonContext":{"appId":7},"flowInstId":"123"}` {
			t.Fatal("candidate provider did not receive complete changeSet")
		}
		return resource.Resolution{Groups: [][]resource.Reference{{{Kind: "application", ID: "7"}}}}, nil
	})))
	input := CreateInput{SceneKey: "recovery", ChangeSet: json.RawMessage(`{"flowInstId":"123"}`), IdempotencyKey: "flow-123"}
	if _, _, _, err := service.Create(context.Background(), Scope{}, input); err != nil || calls != 1 || repo.input.ChangeSetJSON != `{"commonContext":{"appId":7},"flowInstId":"123"}` {
		t.Fatalf("complete snapshot was not frozen: %v", err)
	}
	repo.replay, repo.replayErr = PolicyRun{ID: "frozen"}, nil
	if _, _, _, err := service.Create(context.Background(), Scope{}, input); err != nil || calls != 1 {
		t.Fatal("exact retry re-enriched business data")
	}
}

func fixedScenePlugins(enricher resource.ChangeSetEnricher, resolver resource.Resolver) func(context.Context, Scope, string) (ScenePlugins, error) {
	return func(context.Context, Scope, string) (ScenePlugins, error) {
		return ScenePlugins{Enricher: enricher, Resolver: resolver}, nil
	}
}
