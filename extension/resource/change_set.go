package resource

import (
	"context"
	"encoding/json"
)

// ChangeSetEnricher supplies a complete business snapshot before binding selection.
// The host validates and freezes the returned object; the plugin never selects a Spec.
type ChangeSetEnricher interface {
	EnrichChangeSet(context.Context, ChangeSetRequest) (json.RawMessage, error)
}

type ChangeSetRequest struct {
	SceneKey  string          `json:"sceneKey"`
	ChangeSet json.RawMessage `json:"changeSet"`
}
