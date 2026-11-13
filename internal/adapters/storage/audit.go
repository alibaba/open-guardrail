package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/alibaba/open-guardrail/internal/application/audit"
	"github.com/pressly/goose/v3"
)

type AuditEvents struct {
	db      *sql.DB
	dialect goose.Dialect
}

func (s *Store) Audits() *AuditEvents { return &AuditEvents{db: s.db, dialect: s.driver.dialect} }

func (s *AuditEvents) ListAuditEvents(ctx context.Context, scope audit.Scope, filters audit.Filters, limit int, before int64) ([]audit.Event, bool, error) {
	// Keep the bound in bytes on every backend before transferring detail text.
	size := "OCTET_LENGTH(details_json)"
	if s.dialect == goose.DialectSQLite3 {
		size = "LENGTH(CAST(details_json AS BLOB))"
	}
	// Unscoped historical events are not attributed to an arbitrary project.
	query := fmt.Sprintf(`SELECT id,event_type,actor_id,occurred_at,resource_type,resource_id,CASE WHEN %s<=%d THEN details_json ELSE NULL END,
 CASE WHEN details_json IS NOT NULL AND %s>%d THEN 1 ELSE 0 END FROM audit_event
 WHERE workspace_id=(SELECT id FROM workspace WHERE workspace_id=$1)
 AND project_id=(SELECT id FROM project WHERE project_id=$2 AND workspace_id=(SELECT id FROM workspace WHERE workspace_id=$3))`, size, audit.MaxDetailsBytes, size, audit.MaxDetailsBytes)
	args := []any{scope.WorkspaceID, scope.ProjectID, scope.WorkspaceID}
	clauses := []string{}
	add := func(column, operator string, value any) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf("%s%s$%d", column, operator, len(args)))
	}
	if filters.EventType != "" {
		add("event_type", "=", filters.EventType)
	}
	if filters.ActorID != "" {
		add("actor_id", "=", filters.ActorID)
	}
	if filters.ResourceType != "" {
		add("resource_type", "=", filters.ResourceType)
	}
	if filters.ResourceID != "" {
		add("resource_id", "=", filters.ResourceID)
	}
	if filters.From != nil {
		add("occurred_at", ">=", *filters.From)
	}
	if filters.To != nil {
		add("occurred_at", "<", *filters.To)
	}
	if before > 0 {
		add("id", "<", before)
	}
	if len(clauses) > 0 {
		query += " AND " + strings.Join(clauses, " AND ")
	}
	args = append(args, limit+1)
	query += fmt.Sprintf(" ORDER BY id DESC LIMIT $%d", len(args))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, fmt.Errorf("list audit events: %w: %w", audit.ErrUnavailable, err)
	}
	defer rows.Close()
	items := []audit.Event{}
	for rows.Next() {
		var item audit.Event
		var id int64
		var unavailable int
		var kind, resourceID, details sql.NullString
		if err := rows.Scan(&id, &item.EventType, &item.ActorID, &item.OccurredAt, &kind, &resourceID, &details, &unavailable); err != nil {
			return nil, false, fmt.Errorf("read audit event: %w: %w", audit.ErrUnavailable, err)
		}
		if len(items) == limit {
			return items, true, nil
		}
		item.ID = strconv.FormatInt(id, 10)
		item.DetailsUnavailable = unavailable != 0
		if kind.Valid {
			item.ResourceType = &kind.String
		}
		if resourceID.Valid {
			item.ResourceID = &resourceID.String
		}
		if details.Valid {
			item.Details = json.RawMessage(details.String)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("read audit events: %w: %w", audit.ErrUnavailable, err)
	}
	return items, false, nil
}
