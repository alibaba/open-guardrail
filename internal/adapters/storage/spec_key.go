package storage

import (
	"context"
	"database/sql"
	"errors"

	"github.com/alibaba/open-guardrail/internal/application/spec"
	specdomain "github.com/alibaba/open-guardrail/internal/domain/spec"
)

// Old policies retain their current active (or latest published, then Draft)
// identity. Inspect at most one document per legacy policy, not its history.
const legacySpecDocument = `COALESCE(a.yaml,
 (SELECT r.yaml FROM policy_revision r WHERE r.policy_id=p.id AND r.status='PUBLISHED' ORDER BY r.revision_number DESC LIMIT 1),
 (SELECT r.yaml FROM policy_revision r WHERE r.policy_id=p.id AND r.status='DRAFT' LIMIT 1),'')`

// The caller holds lockSpecProject before checking names and acquiring Policy locks.
// This also serializes claims against old rows that have not registered a key yet.
func checkSpecKeyAvailable(ctx context.Context, tx *sql.Tx, scope spec.Scope, scene, name, except string) error {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT policy_id FROM policy
 WHERE workspace_id=(SELECT id FROM workspace WHERE workspace_id=$1)
 AND project_id=(SELECT id FROM project WHERE project_id=$2)
 AND scene_key=$3 AND spec_key=$4 AND policy_id<>$5`, scope.WorkspaceID, scope.ProjectID, scene, name, except).Scan(&id)
	if err == nil {
		return spec.ErrSpecKeyConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return specError(err)
	}
	rows, err := tx.QueryContext(ctx, "SELECT "+legacySpecDocument+policyModelFrom+`
 WHERE p.workspace_id=(SELECT id FROM workspace WHERE workspace_id=$1)
 AND p.project_id=(SELECT id FROM project WHERE project_id=$2)
 AND p.scene_key=$3 AND p.spec_key IS NULL AND p.policy_id<>$4`, scope.WorkspaceID, scope.ProjectID, scene, except)
	if err != nil {
		return specError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return specError(err)
		}
		if previous, err := specdomain.Name(raw); err == nil && previous == name {
			return spec.ErrSpecKeyConflict
		}
	}
	return specError(rows.Err())
}

func legacyPolicySpecKey(ctx context.Context, tx *sql.Tx, id string) (string, error) {
	var raw string
	if err := tx.QueryRowContext(ctx, "SELECT "+legacySpecDocument+policyModelFrom+" WHERE p.policy_id=$1", id).Scan(&raw); err != nil {
		return "", specError(err)
	}
	name, _ := specdomain.Name(raw)
	return name, nil
}

func bindPolicySpecKey(ctx context.Context, tx *sql.Tx, scope spec.Scope, policy *spec.Policy, candidate string) error {
	if policy.SpecKey == "" {
		previous, err := legacyPolicySpecKey(ctx, tx, policy.ID)
		if err != nil {
			return err
		}
		if previous != "" {
			if candidate != "" && candidate != previous {
				return spec.ErrSpecKeyMismatch
			}
			candidate = previous
		}
		if candidate == "" {
			return nil
		}
		if err := checkSpecKeyAvailable(ctx, tx, scope, policy.SceneKey, candidate, policy.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE policy SET spec_key=$1 WHERE policy_id=$2 AND spec_key IS NULL", candidate, policy.ID); err != nil {
			return specError(err)
		}
		policy.SpecKey = candidate
	}
	if candidate != "" && candidate != policy.SpecKey {
		return spec.ErrSpecKeyMismatch
	}
	return nil
}
