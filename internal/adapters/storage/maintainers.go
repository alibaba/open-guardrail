package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"time"

	"github.com/alibaba/open-guardrail/internal/application/spec"
)

func creatorMaintainers(scope spec.Scope) string {
	raw, _ := json.Marshal([]string{scope.ActorID})
	return string(raw)
}

func lockMaintainedPolicy(ctx context.Context, tx *sql.Tx, scope spec.Scope, id string) (spec.Policy, error) {
	p, err := lockPolicyModel(ctx, tx, scope, id)
	if err != nil {
		return spec.Policy{}, err
	}
	if !scope.Unrestricted && !p.CanMaintain(scope.ActorID) {
		return spec.Policy{}, spec.ErrForbidden
	}
	return p, nil
}

func (s *Specs) SetPolicyMaintainers(ctx context.Context, scope spec.Scope, id string, maintainers []string) (spec.Policy, error) {
	if !spec.ValidMaintainers(maintainers) {
		return spec.Policy{}, spec.ErrInvalid
	}
	maintainers = slices.Clone(maintainers)
	slices.Sort(maintainers)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return spec.Policy{}, specError(err)
	}
	defer tx.Rollback()
	p, err := lockMaintainedPolicy(ctx, tx, scope, id)
	if err != nil {
		return spec.Policy{}, err
	}
	before := slices.Clone(p.Maintainers)
	slices.Sort(before)
	if slices.Equal(before, maintainers) {
		return p, nil
	}
	raw, _ := json.Marshal(maintainers)
	if _, err := tx.ExecContext(ctx, "UPDATE policy SET maintainers_json=$1 WHERE policy_id=$2", string(raw), id); err != nil {
		return spec.Policy{}, specError(err)
	}
	details, _ := json.Marshal(struct {
		Before []string `json:"before"`
		After  []string `json:"after"`
	}{p.Maintainers, maintainers})
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_event(event_type,actor_id,workspace_id,project_id,occurred_at,resource_type,resource_id,details_json)
 VALUES('policy_maintainers_updated',$1,(SELECT id FROM workspace WHERE workspace_id=$2),(SELECT id FROM project WHERE project_id=$3),$4,'policy',$5,$6)`, scope.ActorID, scope.WorkspaceID, scope.ProjectID, time.Now().Unix(), id, string(details)); err != nil {
		return spec.Policy{}, specError(err)
	}
	if err := tx.Commit(); err != nil {
		return spec.Policy{}, specError(err)
	}
	p.Maintainers = maintainers
	return p, nil
}
