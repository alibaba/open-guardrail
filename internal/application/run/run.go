// Package run defines Run scope and errors.
package run

import "errors"

var (
	ErrBindingConflict     = errors.New("business resources have incomplete or conflicting Policy bindings")
	ErrInvalid             = errors.New("invalid Run request")
	ErrNotFound            = errors.New("Run or Scene not found")
	ErrSpecNotPublished    = errors.New("Scene has no active Policy Revision")
	ErrSpecInvalid         = errors.New("selected draft Spec is invalid")
	ErrIdempotencyConflict = errors.New("idempotency key was already used for another request")
	ErrBusinessIDConflict  = errors.New("business ID was already used for another Run request")
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
	Operator                 string
	SubmissionCredentialHash string
	CanSelectRevision        bool
}
