package run

import "github.com/alibaba/open-guardrail/internal/domain/resource"

// Selection stores optional details beyond the Policy/Revision already on a Run.
// New Runs record it only for matched bindings; absent evidence stays absent.
type Selection struct {
	Source    string               `json:"source"`
	Resources []resource.Reference `json:"resources,omitempty"`
	// Only binding selections have a revisionMode: active or pinned.
	RevisionMode string `json:"revisionMode,omitempty"`
}
