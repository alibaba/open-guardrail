// Package run defines Run scope and errors.
package run

import "errors"

var (
	ErrInvalid             = errors.New("invalid Run request")
	ErrNotFound            = errors.New("Run or Scene not found")
	ErrSpecNotPublished    = errors.New("Scene has no active Policy Revision")
	ErrIdempotencyConflict = errors.New("idempotency key was already used for another request")
	ErrCompleted           = errors.New("Run is already finished")
	ErrExpired             = errors.New("Run has expired")
	ErrUnauthorized        = errors.New("Run credential rejected")
	ErrUnavailable         = errors.New("Run storage unavailable")
)

const (
	DefaultListLimit = 20
	MaxListLimit     = 100
)

type Scope struct {
	WorkspaceID              string
	ProjectID                string
	ActorID                  string
	SubmissionCredentialHash string
	CanSelectRevision        bool
}
