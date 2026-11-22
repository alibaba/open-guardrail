// Package resource defines resource references and optional binding candidates.
package resource

import (
	"context"
	"encoding/json"
	"errors"

	coreresource "github.com/alibaba/open-guardrail/internal/domain/resource"
)

const MaxResources = coreresource.MaxResources
const SPIVersion = "v1alpha1"

var ErrInvalid = errors.New("business resource reference is invalid")
var ErrUnavailable = errors.New("binding candidate provider unavailable")

// Reference shares the core binding identity and validation.
type Reference = coreresource.Reference

type ResolveRequest struct {
	SceneKey  string          `json:"sceneKey"`
	ChangeSet json.RawMessage `json:"changeSet"`
}

// Groups are ordered by precedence. The host selects the first group with a
// binding. All resources in that group must have the same complete selection.
// An empty groups array means no business resources; dependency failures are errors.
type Resolution struct {
	Groups [][]Reference `json:"groups"`
}

func (r Resolution) Valid() bool {
	if r.Groups == nil || len(r.Groups) > MaxResources {
		return false
	}
	n := 0
	for _, group := range r.Groups {
		if len(group) == 0 {
			return false
		}
		seen := map[Reference]bool{}
		for _, ref := range group {
			n++
			if n > MaxResources || !ref.Valid() || seen[ref] {
				return false
			}
			seen[ref] = true
		}
	}
	return true
}

// Resolver provides ordered binding candidates. It never supplies Spec contents
// or writes OpenGuardrail objects. Implementations must honor cancellation.
type Resolver interface {
	ResolveResources(context.Context, ResolveRequest) (Resolution, error)
}
