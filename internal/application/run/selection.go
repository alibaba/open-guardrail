package run

import "github.com/alibaba/open-guardrail/internal/domain/resource"

// Selection stores optional details beyond the Policy/Revision already on a Run.
// Draft Runs also retain the revision status at creation.
type Selection struct {
	Source         string               `json:"source"`
	Resources      []resource.Reference `json:"resources,omitempty"`
	RevisionStatus string               `json:"revisionStatus,omitempty"`
	// Only binding selections have a revisionMode: active or pinned.
	RevisionMode string `json:"revisionMode,omitempty"`
}
