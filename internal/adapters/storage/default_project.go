package storage

import (
	"context"
	"database/sql"
	"errors"
)

// Prepare defaults independently of local identity and schema migration mode.
// Separate commits allow a restart to finish partial initialization and avoid
// keeping a transaction open after a duplicate insert on MySQL.
func (s *Store) ensureDefaultProject(ctx context.Context) error {
	const query = "SELECT 1 FROM workspace w JOIN project p ON p.workspace_id=w.id WHERE w.workspace_id=$1 AND p.project_id=$2"
	var exists int
	err := s.db.QueryRowContext(ctx, query, "default", "default").Scan(&exists)
	if err == nil || !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	statements := []struct {
		query string
		args  []any
	}{
		{"INSERT INTO workspace(workspace_id,name) VALUES($1,$2)" + s.driver.conflictClause("workspace_id"), []any{"default", "Default"}},
		{"INSERT INTO project(project_id,workspace_id,name) SELECT $1,id,$2 FROM workspace WHERE workspace_id=$3" + s.driver.conflictClause("project_id"), []any{"default", "Default", "default"}},
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement.query, statement.args...); err != nil && !s.driver.isDuplicate(err) {
			return err
		}
	}
	return s.db.QueryRowContext(ctx, query, "default", "default").Scan(&exists)
}
