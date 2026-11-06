package spec

import "errors"

var ErrSceneAlreadyInitialized = errors.New("scene already initialized")

type SceneModel struct {
	Key             string  `json:"key"`
	Name            string  `json:"name"`
	DefaultPolicyID *string `json:"defaultPolicyId,omitempty"`
	RunTTLSeconds   int64   `json:"runTtlSeconds"`
}
type Policy struct {
	ID               string  `json:"id"`
	SceneKey         string  `json:"sceneKey"`
	Name             string  `json:"name"`
	LifecycleState   string  `json:"lifecycleState"`
	ActiveRevisionID *string `json:"activeRevisionId,omitempty"`
}
type PolicyRevision struct {
	ID             string  `json:"id"`
	PolicyID       string  `json:"policyId"`
	Status         string  `json:"status"`
	RevisionNumber *int64  `json:"revisionNumber,omitempty"`
	YAML           string  `json:"yaml"`
	Digest         *string `json:"digest,omitempty"`
	UpdatedBy      string  `json:"updatedBy"`
	UpdatedAt      int64   `json:"updatedAt"`
	PublishedAt    *int64  `json:"publishedAt,omitempty"`
	PublishedBy    *string `json:"publishedBy,omitempty"`
}
type InitializedScene struct {
	Scene    SceneModel     `json:"scene"`
	Policy   Policy         `json:"policy"`
	Revision PolicyRevision `json:"revision"`
}

// RevisionSummary lists published versions without YAML.
type RevisionSummary struct {
	ID             string `json:"id"`
	PolicyID       string `json:"policyId"`
	RevisionNumber int64  `json:"revisionNumber"`
	Digest         string `json:"digest"`
	PublishedAt    int64  `json:"publishedAt"`
	PublishedBy    string `json:"publishedBy"`
}
type RevisionPage struct {
	Items      []RevisionSummary `json:"items"`
	NextBefore *int64            `json:"nextBefore,omitempty"`
}
