package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	runapp "github.com/alibaba/open-guardrail/internal/application/run"
	"github.com/alibaba/open-guardrail/internal/application/spec"
)

type Specs struct {
	db     *sql.DB
	driver driverDefinition
}
type Runs struct {
	db     *sql.DB
	driver driverDefinition
}

func (s *Store) Specs() *Specs { return &Specs{db: s.db, driver: s.driver} }
func (s *Store) Runs() *Runs   { return &Runs{db: s.db, driver: s.driver} }

type rowQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func specError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("policy storage: %w: %w", spec.ErrNotFound, err)
	}
	return fmt.Errorf("policy storage: %w: %w", spec.ErrUnavailable, err)
}
func runError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("run storage: %w: %w", runapp.ErrNotFound, err)
	}
	return fmt.Errorf("run storage: %w: %w", runapp.ErrUnavailable, err)
}
func specAudit(ctx context.Context, tx *sql.Tx, scope spec.Scope, event, resourceType, resourceID string) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO audit_event(event_type,actor_id,workspace_id,project_id,occurred_at,resource_type,resource_id) VALUES($1,$2,(SELECT id FROM workspace WHERE workspace_id=$3),(SELECT id FROM project WHERE project_id=$4),$5,$6,$7)", event, scope.ActorID, scope.WorkspaceID, scope.ProjectID, time.Now().Unix(), resourceType, resourceID)
	return specError(err)
}

// Terminal credentials remain authenticatable for exact idempotent retries.
func (s *Runs) AuthenticateSubmissionToken(ctx context.Context, runID, tokenHash string) (runapp.Scope, error) {
	var scope runapp.Scope
	err := s.db.QueryRowContext(ctx, `SELECT w.workspace_id,p.project_id FROM run_submission_credential c JOIN guardrail_run r ON r.id=c.run_id JOIN workspace w ON w.id=r.workspace_id JOIN project p ON p.id=r.project_id WHERE r.run_id=$1 AND c.token_hash=$2`, runID, tokenHash).Scan(&scope.WorkspaceID, &scope.ProjectID)
	if errors.Is(err, sql.ErrNoRows) {
		return runapp.Scope{}, runapp.ErrUnauthorized
	}
	if err != nil {
		return runapp.Scope{}, runapp.ErrUnavailable
	}
	scope.ActorID = "run-token:" + runID
	scope.SubmissionCredentialHash = tokenHash
	return scope, nil
}
