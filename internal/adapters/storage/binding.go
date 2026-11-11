package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	runapp "github.com/alibaba/open-guardrail/internal/application/run"
	"github.com/alibaba/open-guardrail/internal/application/spec"
	"github.com/alibaba/open-guardrail/internal/domain/resource"
)

func lockBindingScene(ctx context.Context, tx *sql.Tx, scope spec.Scope, scene string) error {
	r, err := tx.ExecContext(ctx, "UPDATE scene SET name=name WHERE workspace_id=(SELECT id FROM workspace WHERE workspace_id=$1) AND project_id=(SELECT id FROM project WHERE project_id=$2) AND scene_key=$3", scope.WorkspaceID, scope.ProjectID, scene)
	if err != nil {
		return specError(err)
	}
	n, err := r.RowsAffected()
	if err != nil {
		return specError(err)
	}
	if n == 0 {
		return spec.ErrNotFound
	}
	return nil
}

func (s *Specs) PutBinding(ctx context.Context, scope spec.Scope, scene string, b spec.Binding) (spec.Binding, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return spec.Binding{}, specError(err)
	}
	defer tx.Rollback()
	// Run creation, binding writes and archival all lock Scene before Policy.
	if err = lockBindingScene(ctx, tx, scope, scene); err != nil {
		return spec.Binding{}, err
	}
	before, err := bindingByResource(ctx, tx, scope, scene, b.Resource)
	if err != nil {
		return spec.Binding{}, err
	}
	p, err := lockPolicyModel(ctx, tx, scope, b.PolicyID)
	if err != nil {
		return spec.Binding{}, err
	}
	if p.SceneKey != scene {
		return spec.Binding{}, spec.ErrInvalid
	}
	if p.LifecycleState != "ACTIVE" {
		return spec.Binding{}, spec.ErrConflict
	}
	var revision any
	if b.PolicyRevisionID != nil {
		r, err := revisionByID(ctx, tx, scope, *b.PolicyRevisionID)
		if err != nil {
			return spec.Binding{}, err
		}
		if r.PolicyID != p.ID || r.Status != "PUBLISHED" {
			return spec.Binding{}, spec.ErrInvalid
		}
		revision = *b.PolicyRevisionID
	}
	if before != nil && before.PolicyID == b.PolicyID && sameRevision(before.PolicyRevisionID, b.PolicyRevisionID) {
		return b, nil
	}
	// Scene locking makes delete+insert one atomic, portable upsert. The unique
	// resource key also protects against writers outside this application.
	if _, err = tx.ExecContext(ctx, "DELETE FROM policy_binding WHERE workspace_id=(SELECT id FROM workspace WHERE workspace_id=$1) AND project_id=(SELECT id FROM project WHERE project_id=$2) AND scene_key=$3 AND resource_kind=$4 AND resource_id=$5", scope.WorkspaceID, scope.ProjectID, scene, b.Resource.Kind, b.Resource.ID); err != nil {
		return spec.Binding{}, specError(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO policy_binding(workspace_id,project_id,scene_key,resource_kind,resource_id,policy_id,policy_revision_id)
	 VALUES((SELECT id FROM workspace WHERE workspace_id=$1),(SELECT id FROM project WHERE project_id=$2),$3,$4,$5,(SELECT id FROM policy WHERE policy_id=$6),(SELECT id FROM policy_revision WHERE policy_revision_id=$7))`, scope.WorkspaceID, scope.ProjectID, scene, b.Resource.Kind, b.Resource.ID, b.PolicyID, revision); err != nil {
		return spec.Binding{}, specError(err)
	}
	if err = auditBindingChange(ctx, tx, scope, scene, before, &b); err != nil {
		return spec.Binding{}, err
	}
	return b, specError(tx.Commit())
}

func (s *Specs) DeleteBinding(ctx context.Context, scope spec.Scope, scene string, ref resource.Reference) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return specError(err)
	}
	defer tx.Rollback()
	if err = lockBindingScene(ctx, tx, scope, scene); err != nil {
		return err
	}
	before, err := bindingByResource(ctx, tx, scope, scene, ref)
	if err != nil {
		return err
	}
	if before == nil {
		return nil
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM policy_binding WHERE workspace_id=(SELECT id FROM workspace WHERE workspace_id=$1) AND project_id=(SELECT id FROM project WHERE project_id=$2) AND scene_key=$3 AND resource_kind=$4 AND resource_id=$5", scope.WorkspaceID, scope.ProjectID, scene, ref.Kind, ref.ID)
	if err != nil {
		return specError(err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return specError(err)
	}
	if n == 0 {
		return nil
	}
	if err := auditBindingChange(ctx, tx, scope, scene, before, nil); err != nil {
		return err
	}
	return specError(tx.Commit())
}

func (s *Specs) ListBindings(ctx context.Context, scope spec.Scope, scene string, limit int, after *resource.Reference) (spec.BindingPage, error) {
	if _, err := s.GetSceneModel(ctx, scope, scene); err != nil {
		return spec.BindingPage{}, err
	}
	query := `SELECT b.resource_kind,b.resource_id,p.policy_id,r.policy_revision_id FROM policy_binding b JOIN policy p ON p.id=b.policy_id LEFT JOIN policy_revision r ON r.id=b.policy_revision_id
	 WHERE b.workspace_id=(SELECT id FROM workspace WHERE workspace_id=$1) AND b.project_id=(SELECT id FROM project WHERE project_id=$2) AND b.scene_key=$3`
	args := []any{scope.WorkspaceID, scope.ProjectID, scene}
	if after != nil {
		query += " AND (b.resource_kind>$4 OR (b.resource_kind=$5 AND b.resource_id>$6))"
		args = append(args, after.Kind, after.Kind, after.ID)
	}
	query += fmt.Sprintf(" ORDER BY b.resource_kind,b.resource_id LIMIT $%d", len(args)+1)
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return spec.BindingPage{}, specError(err)
	}
	defer rows.Close()
	page := spec.BindingPage{Items: []spec.Binding{}}
	for rows.Next() {
		var b spec.Binding
		if err := rows.Scan(&b.Resource.Kind, &b.Resource.ID, &b.PolicyID, &b.PolicyRevisionID); err != nil {
			return spec.BindingPage{}, specError(err)
		}
		if len(page.Items) == limit {
			last := page.Items[limit-1].Resource
			page.NextAfter = &last
			break
		}
		page.Items = append(page.Items, b)
	}
	return page, specError(rows.Err())
}

func bindingByResource(ctx context.Context, tx *sql.Tx, scope spec.Scope, scene string, ref resource.Reference) (*spec.Binding, error) {
	b := &spec.Binding{Resource: ref}
	err := tx.QueryRowContext(ctx, `SELECT p.policy_id,r.policy_revision_id FROM policy_binding b JOIN policy p ON p.id=b.policy_id LEFT JOIN policy_revision r ON r.id=b.policy_revision_id
 WHERE b.workspace_id=(SELECT id FROM workspace WHERE workspace_id=$1) AND b.project_id=(SELECT id FROM project WHERE project_id=$2) AND b.scene_key=$3 AND b.resource_kind=$4 AND b.resource_id=$5`, scope.WorkspaceID, scope.ProjectID, scene, ref.Kind, ref.ID).Scan(&b.PolicyID, &b.PolicyRevisionID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return b, specError(err)
}
func sameRevision(a, b *string) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

func auditBindingChange(ctx context.Context, tx *sql.Tx, scope spec.Scope, scene string, before, after *spec.Binding) error {
	event, ref := "policy_binding_updated", after
	if before == nil {
		event = "policy_binding_created"
	}
	if after == nil {
		event = "policy_binding_deleted"
		ref = before
	}
	details, err := json.Marshal(struct {
		SceneKey string        `json:"sceneKey"`
		Before   *spec.Binding `json:"before"`
		After    *spec.Binding `json:"after"`
	}{scene, before, after})
	if err != nil {
		return specError(err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_event(event_type,actor_id,workspace_id,project_id,occurred_at,resource_type,resource_id,details_json)
 VALUES($1,$2,(SELECT id FROM workspace WHERE workspace_id=$3),(SELECT id FROM project WHERE project_id=$4),$5,'policy-binding',$6,$7)`, event, scope.ActorID, scope.WorkspaceID, scope.ProjectID, time.Now().Unix(), modelDigest(scene+"\n"+ref.Resource.Kind+"\n"+ref.Resource.ID), string(details))
	return specError(err)
}

type bindingSelection struct {
	PolicyID, RevisionID string
	Resources            []resource.Reference
}

// Select persisted references inside the Scene-locked Run creation transaction.
// A partially bound or conflicting resource set cannot use the default Policy.
func selectBinding(ctx context.Context, tx *sql.Tx, scope runapp.Scope, scene string, refs []resource.Reference) (bindingSelection, error) {
	if len(refs) == 0 {
		return bindingSelection{}, nil
	}
	query := `SELECT p.policy_id,COALESCE(r.policy_revision_id,'') FROM policy_binding b JOIN policy p ON p.id=b.policy_id LEFT JOIN policy_revision r ON r.id=b.policy_revision_id
 WHERE b.workspace_id=(SELECT id FROM workspace WHERE workspace_id=$1) AND b.project_id=(SELECT id FROM project WHERE project_id=$2) AND b.scene_key=$3 AND (`
	args := []any{scope.WorkspaceID, scope.ProjectID, scene}
	terms := make([]string, 0, len(refs))
	for _, ref := range refs {
		terms = append(terms, fmt.Sprintf("(b.resource_kind=$%d AND b.resource_id=$%d)", len(args)+1, len(args)+2))
		args = append(args, ref.Kind, ref.ID)
	}
	rows, err := tx.QueryContext(ctx, query+strings.Join(terms, " OR ")+")", args...)
	if err != nil {
		return bindingSelection{}, runError(err)
	}
	defer rows.Close()
	count, policy, revision := 0, "", ""
	for rows.Next() {
		var p, r string
		if err := rows.Scan(&p, &r); err != nil {
			return bindingSelection{}, runError(err)
		}
		if count > 0 && (p != policy || r != revision) {
			return bindingSelection{}, runapp.ErrBindingConflict
		}
		policy, revision, count = p, r, count+1
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return bindingSelection{}, runError(err)
	}
	if count == 0 {
		return bindingSelection{}, nil
	}
	if count != len(refs) {
		return bindingSelection{}, runapp.ErrBindingConflict
	}
	return bindingSelection{PolicyID: policy, RevisionID: revision, Resources: append([]resource.Reference(nil), refs...)}, nil
}
