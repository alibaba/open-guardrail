package httpclient

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/alibaba/open-guardrail/extension/resource"
)

var _ resource.Resolver = (*Client)(nil)
var _ resource.ChangeSetEnricher = (*Client)(nil)

func (c *Client) EnrichChangeSet(ctx context.Context, request resource.ChangeSetRequest) (json.RawMessage, error) {
	client := *c
	http := *c.http
	http.Timeout = 8 * time.Second
	client.http = &http
	result, err := client.invoke(ctx, "changeset.enrich", request)
	if errors.Is(err, errInvalidArgument) {
		return nil, resource.ErrInvalid
	}
	if err != nil || !exactKeys(result, []string{"changeSet"}) {
		return nil, resource.ErrUnavailable
	}
	var response struct {
		ChangeSet json.RawMessage `json:"changeSet"`
	}
	if strictJSON(result, &response) != nil || len(response.ChangeSet) == 0 {
		return nil, resource.ErrUnavailable
	}
	return response.ChangeSet, nil
}

func (c *Client) ResolveResources(ctx context.Context, request resource.ResolveRequest) (resource.Resolution, error) {
	// Keep business lookups within the host's request deadline. Never retry implicitly.
	client := *c
	http := *c.http
	http.Timeout = 8 * time.Second
	client.http = &http
	result, err := client.invoke(ctx, "bindings.candidates", request)
	if errors.Is(err, errInvalidArgument) {
		return resource.Resolution{}, resource.ErrInvalid
	}
	var resolution resource.Resolution
	if err != nil || !exactKeys(result, []string{"groups"}) || strictJSON(result, &resolution) != nil || !resolution.Valid() {
		return resource.Resolution{}, resource.ErrUnavailable
	}
	// Guard encoding/json's case-insensitive struct matching at every typed level.
	var raw struct {
		Groups [][]json.RawMessage `json:"groups"`
	}
	_ = json.Unmarshal(result, &raw)
	for _, group := range raw.Groups {
		for _, ref := range group {
			if !exactKeys(ref, []string{"kind", "id"}) {
				return resource.Resolution{}, resource.ErrUnavailable
			}
		}
	}
	return resolution, nil
}
