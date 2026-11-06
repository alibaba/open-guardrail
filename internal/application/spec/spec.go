// Package spec defines Policy management scope and errors.
package spec

import "errors"

var (
	ErrInvalid     = errors.New("invalid Scene or Policy request")
	ErrNotFound    = errors.New("Scene, Policy or Revision not found")
	ErrConflict    = errors.New("Policy state changed; reload before retrying")
	ErrUnavailable = errors.New("Policy storage unavailable")
)

// Scope is derived from an authenticated administrator, never request fields.
type Scope struct{ WorkspaceID, ProjectID, ActorID string }
