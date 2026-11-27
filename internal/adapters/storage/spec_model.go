package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/alibaba/open-guardrail/extension/resource"
	"strconv"
	"time"

	"github.com/alibaba/open-guardrail/internal/application/spec"
	opaque "github.com/alibaba/open-guardrail/internal/application/token"
	specdomain "github.com/alibaba/open-guardrail/internal/domain/spec"
)

func scanSceneModel(row scanner) (spec.SceneModel, error) {
	var m spec.SceneModel
	var defaultID, createdBy sql.NullString
	var plugins []byte
	err := row.Scan(&m.Key, &m.Name, &defaultID, &m.RunTTLSeconds, &plugins, &createdBy)
	if err != nil {
		return spec.SceneModel{}, specError(err)
	}
	if plugins != nil {
		if err := json.Unmarshal(plugins, &m.Plugins); err != nil || !m.Plugins.Valid() {
			return spec.SceneModel{}, spec.ErrUnavailable
		}
	}
	if defaultID.Valid {
		m.DefaultPolicyID = &defaultID.String
	}
	if createdBy.Valid {
		m.CreatedBy = &createdBy.String
	}
	return m, nil
}
func (s *Specs) GetSceneModel(ctx context.Context, scope spec.Scope, key string) (spec.SceneModel, error) {
	return scanSceneModel(s.db.QueryRowContext(ctx, "SELECT "+sceneModelColumns+" FROM scene WHERE workspace_id=(SELECT id FROM workspace WHERE workspace_id=$1) AND project_id=(SELECT id FROM project WHERE project_id=$2) AND scene_key=$3", scope.WorkspaceID, scope.ProjectID, key))
}
func (s *Specs) ListSceneModels(ctx context.Context, scope spec.Scope) ([]spec.SceneModel, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+sceneModelColumns+" FROM scene WHERE workspace_id=(SELECT id FROM workspace WHERE workspace_id=$1) AND project_id=(SELECT id FROM project WHERE project_id=$2) ORDER BY scene_key", scope.WorkspaceID, scope.ProjectID)
	if err != nil {
		return nil, specError(err)
	}
	defer rows.Close()
	result := []spec.SceneModel{}
	for rows.Next() {
		scene, err := scanSceneModel(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, scene)
	}
	return result, specError(rows.Err())
}
func scanPolicyModel(row scanner) (spec.Policy, error) {
	var p spec.Policy
	var activeID, specKey, maintainers sql.NullString
	err := row.Scan(&p.ID, &p.SceneKey, &p.Name, &p.LifecycleState, &activeID, &specKey, &maintainers)
	if err != nil {
		return spec.Policy{}, specError(err)
	}
	if activeID.Valid {
		p.ActiveRevisionID = &activeID.String
	}
	p.SpecKey = specKey.String
	p.Maintainers = []string{}
	if maintainers.Valid && (json.Unmarshal([]byte(maintainers.String), &p.Maintainers) != nil || !spec.ValidMaintainers(p.Maintainers)) {
		return spec.Policy{}, spec.ErrUnavailable
	}

	return p, nil
}

const policyModelColumns = "p.policy_id,p.scene_key,p.name,p.lifecycle_state,a.policy_revision_id,p.spec_key,p.maintainers_json"
const policyModelFrom = " FROM policy p LEFT JOIN policy_revision a ON a.id=p.active_revision_id"
const sceneModelColumns = "scene_key,name,(SELECT policy_id FROM policy WHERE id=scene.default_policy_id),run_ttl_seconds,plugins_json,created_by"

func policyByID(ctx context.Context, q rowQueryer, scope spec.Scope, id string) (spec.Policy, error) {
	return scanPolicyModel(q.QueryRowContext(ctx, "SELECT "+policyModelColumns+policyModelFrom+" WHERE p.policy_id=$1 AND p.workspace_id=(SELECT id FROM workspace WHERE workspace_id=$2) AND p.project_id=(SELECT id FROM project WHERE project_id=$3)", id, scope.WorkspaceID, scope.ProjectID))
}
func (s *Specs) GetPolicy(ctx context.Context, scope spec.Scope, id string) (spec.Policy, error) {
	return policyByID(ctx, s.db, scope, id)
}
func scanPolicyRevision(row scanner) (spec.PolicyRevision, error) {
	var r spec.PolicyRevision
	var metadata sql.NullString
	err := row.Scan(&r.ID, &r.PolicyID, &r.RevisionNumber, &r.YAML, &r.Digest, &r.PublishedAt, &r.PublishedBy, &r.Status, &r.UpdatedBy, &r.UpdatedAt, &metadata)
	if err != nil {
		return spec.PolicyRevision{}, specError(err)
	}
	if metadata.Valid {
		r.Metadata = json.RawMessage(metadata.String)
	}
	return r, nil
}

const revisionModelColumns = "r.policy_revision_id,p.policy_id,r.revision_number,r.yaml,r.digest,r.published_at,r.published_by,r.status,r.updated_by,r.updated_at,r.metadata_json"
const revisionModelFrom = " FROM policy_revision r JOIN policy p ON p.id=r.policy_id"

func revisionByID(ctx context.Context, q rowQueryer, scope spec.Scope, id string) (spec.PolicyRevision, error) {
	return scanPolicyRevision(q.QueryRowContext(ctx, "SELECT "+revisionModelColumns+revisionModelFrom+" WHERE r.policy_revision_id=$1 AND p.workspace_id=(SELECT id FROM workspace WHERE workspace_id=$2) AND p.project_id=(SELECT id FROM project WHERE project_id=$3)", id, scope.WorkspaceID, scope.ProjectID))
}
func (s *Specs) GetPolicyRevision(ctx context.Context, scope spec.Scope, id string) (spec.PolicyRevision, error) {
	return revisionByID(ctx, s.db, scope, id)
}
func (s *Specs) ListPolicies(ctx context.Context, scope spec.Scope, sceneKey string) ([]spec.Policy, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+policyModelColumns+policyModelFrom+" WHERE p.workspace_id=(SELECT id FROM workspace WHERE workspace_id=$1) AND p.project_id=(SELECT id FROM project WHERE project_id=$2) AND p.scene_key=$3 ORDER BY p.policy_id", scope.WorkspaceID, scope.ProjectID, sceneKey)
	if err != nil {
		return nil, specError(err)
	}
	defer rows.Close()
	out := []spec.Policy{}
	for rows.Next() {
		p, err := scanPolicyModel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, specError(rows.Err())
}
func (s *Specs) ListPolicyRevisions(ctx context.Context, scope spec.Scope, policyID string, pageSize int, before int64) (spec.RevisionPage, error) {
	if pageSize < 1 || pageSize > 100 || before < 0 {
		return spec.RevisionPage{}, spec.ErrInvalid
	}
	if _, err := policyByID(ctx, s.db, scope, policyID); err != nil {
		return spec.RevisionPage{}, err
	}
	query := "SELECT r.policy_revision_id,p.policy_id,r.revision_number,r.digest,r.published_at,r.published_by FROM policy_revision r JOIN policy p ON p.id=r.policy_id WHERE p.policy_id=$1 AND r.status='PUBLISHED'"
	args := []any{policyID}
	if before > 0 {
		query += " AND r.revision_number<$2"
		args = append(args, before)
	}
	query += " ORDER BY r.revision_number DESC LIMIT $" + strconv.Itoa(len(args)+1)
	args = append(args, pageSize+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return spec.RevisionPage{}, specError(err)
	}
	defer rows.Close()
	page := spec.RevisionPage{Items: []spec.RevisionSummary{}}
	for rows.Next() {
		var item spec.RevisionSummary
		if err := rows.Scan(&item.ID, &item.PolicyID, &item.RevisionNumber, &item.Digest, &item.PublishedAt, &item.PublishedBy); err != nil {
			return spec.RevisionPage{}, specError(err)
		}
		if len(page.Items) == pageSize {
			cursor := page.Items[len(page.Items)-1].RevisionNumber
			page.NextBefore = &cursor
			break
		}
		page.Items = append(page.Items, item)
	}
	return page, specError(rows.Err())
}
func modelDigest(v string) string { sum := sha256.Sum256([]byte(v)); return hex.EncodeToString(sum[:]) }
func recordByKey(ctx context.Context, q rowQueryer, scope spec.Scope, operation, key string) (string, string, error) {
	var digest, resourceID string
	err := q.QueryRowContext(ctx, "SELECT request_digest,resource_id FROM idempotency_record WHERE workspace_id=(SELECT id FROM workspace WHERE workspace_id=$1) AND project_id=(SELECT id FROM project WHERE project_id=$2) AND operation_type=$3 AND idempotency_key=$4", scope.WorkspaceID, scope.ProjectID, operation, key).Scan(&digest, &resourceID)
	if err != nil {
		return "", "", specError(err)
	}
	return digest, resourceID, nil
}
func insertRecord(ctx context.Context, tx *sql.Tx, scope spec.Scope, operation, key, digest, resourceType, resourceID string, now int64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO idempotency_record(workspace_id,project_id,operation_type,idempotency_key,request_digest,resource_type,resource_id,created_at)
 VALUES((SELECT id FROM workspace WHERE workspace_id=$1),(SELECT id FROM project WHERE project_id=$2),$3,$4,$5,$6,$7,$8)`, scope.WorkspaceID, scope.ProjectID, operation, key, digest, resourceType, resourceID, now)
	return specError(err)
}
func lockPolicyModel(ctx context.Context, tx *sql.Tx, scope spec.Scope, id string) (spec.Policy, error) {
	result, err := tx.ExecContext(ctx, "UPDATE policy SET lifecycle_state=lifecycle_state WHERE policy_id=$1 AND workspace_id=(SELECT id FROM workspace WHERE workspace_id=$2) AND project_id=(SELECT id FROM project WHERE project_id=$3)", id, scope.WorkspaceID, scope.ProjectID)
	if err != nil {
		return spec.Policy{}, specError(err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return spec.Policy{}, specError(err)
	}
	if n == 0 {
		return spec.Policy{}, spec.ErrNotFound
	}
	return policyByID(ctx, tx, scope, id)
}
func (s *Specs) CreatePolicy(ctx context.Context, scope spec.Scope, sceneKey, name, specKey, key string, refs ...resource.Reference) (spec.Policy, bool, error) {
	if (specKey != "" && !specdomain.ValidName(specKey)) || !spec.ValidActorID(scope.ActorID) {
		return spec.Policy{}, false, spec.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return spec.Policy{}, false, specError(err)
	}
	defer tx.Rollback()
	if err = lockSpecProject(ctx, tx, scope); err != nil {
		return spec.Policy{}, false, err
	}
	if len(refs) > 1 || (len(refs) == 1 && !refs[0].Valid()) {
		return spec.Policy{}, false, spec.ErrInvalid
	}
	legacyRaw := sceneKey + "\n" + name
	if len(refs) == 1 {
		encoded, _ := json.Marshal(refs[0])
		legacyRaw += "\nresource\n" + string(encoded)
	}
	// Encode field boundaries explicitly, including the immutable Spec key and binding.
	request, _ := json.Marshal(struct {
		SceneKey string
		Name     string
		SpecKey  string
		Refs     []resource.Reference
	}{sceneKey, name, specKey, refs})
	requestDigest := modelDigest(string(request))
	if digest, id, err := recordByKey(ctx, tx, scope, "policy:create", key); err == nil {
		legacy := digest == modelDigest(legacyRaw)
		if digest != requestDigest && !legacy {
			return spec.Policy{}, false, spec.ErrConflict
		}
		p, err := policyByID(ctx, tx, scope, id)
		if err == nil && legacy && specKey != "" {
			// Old callers omitted specKey. An upgraded retry may supply it only
			// when it matches the existing identity; replay never changes a Policy.
			storedKey := p.SpecKey
			if storedKey == "" {
				storedKey, err = legacyPolicySpecKey(ctx, tx, p.ID)
			}
			if err == nil && storedKey != specKey {
				return spec.Policy{}, false, spec.ErrConflict
			}
		}
		return p, false, err
	} else if !errors.Is(err, spec.ErrNotFound) {
		return spec.Policy{}, false, err
	}
	if !specdomain.ValidName(specKey) {
		return spec.Policy{}, false, spec.ErrInvalid
	}
	result, err := tx.ExecContext(ctx, "UPDATE scene SET name=name WHERE workspace_id=(SELECT id FROM workspace WHERE workspace_id=$1) AND project_id=(SELECT id FROM project WHERE project_id=$2) AND scene_key=$3", scope.WorkspaceID, scope.ProjectID, sceneKey)
	if err != nil {
		return spec.Policy{}, false, specError(err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return spec.Policy{}, false, specError(err)
	}
	if n == 0 {
		return spec.Policy{}, false, spec.ErrNotFound
	}
	if err = checkSpecKeyAvailable(ctx, tx, scope, sceneKey, specKey, ""); err != nil {
		return spec.Policy{}, false, err
	}
	p := spec.Policy{ID: opaque.RandomToken("pol_"), SceneKey: sceneKey, Name: name, SpecKey: specKey, LifecycleState: "ACTIVE", Maintainers: []string{scope.ActorID}}
	if _, err = tx.ExecContext(ctx, "INSERT INTO policy(policy_id,workspace_id,project_id,scene_key,name,spec_key,maintainers_json) VALUES($1,(SELECT id FROM workspace WHERE workspace_id=$2),(SELECT id FROM project WHERE project_id=$3),$4,$5,$6,$7)", p.ID, scope.WorkspaceID, scope.ProjectID, sceneKey, name, specKey, creatorMaintainers(scope)); err != nil {
		return spec.Policy{}, false, specError(err)
	}
	if len(refs) == 1 {
		if err = s.insertCreationBinding(ctx, tx, scope, sceneKey, p.ID, refs[0]); err != nil {
			return spec.Policy{}, false, err
		}
	}
	now := time.Now().Unix()
	if err = insertRecord(ctx, tx, scope, "policy:create", key, requestDigest, "policy", p.ID, now); err != nil {
		return spec.Policy{}, false, err
	}
	if err = specAudit(ctx, tx, scope, "policy_created", "policy", p.ID); err != nil {
		return spec.Policy{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return spec.Policy{}, false, specError(err)
	}
	return p, true, nil
}
func draftByPolicy(ctx context.Context, q rowQueryer, scope spec.Scope, id string) (spec.PolicyRevision, error) {
	return scanPolicyRevision(q.QueryRowContext(ctx, "SELECT "+revisionModelColumns+revisionModelFrom+" WHERE p.policy_id=$1 AND r.status='DRAFT' AND p.workspace_id=(SELECT id FROM workspace WHERE workspace_id=$2) AND p.project_id=(SELECT id FROM project WHERE project_id=$3)", id, scope.WorkspaceID, scope.ProjectID))
}
func (s *Specs) GetPolicyDraft(ctx context.Context, scope spec.Scope, id string) (spec.PolicyRevision, error) {
	return draftByPolicy(ctx, s.db, scope, id)
}
func (s *Specs) SavePolicyDraft(ctx context.Context, scope spec.Scope, id, yaml string) (spec.PolicyRevision, error) {
	if len(yaml) > specdomain.MaxYAMLBytes {
		return spec.PolicyRevision{}, spec.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return spec.PolicyRevision{}, specError(err)
	}
	defer tx.Rollback()
	if err = lockSpecProject(ctx, tx, scope); err != nil {
		return spec.PolicyRevision{}, err
	}
	policy, err := lockMaintainedPolicy(ctx, tx, scope, id)
	if err != nil {
		return spec.PolicyRevision{}, err
	}
	if policy.LifecycleState != "ACTIVE" {
		return spec.PolicyRevision{}, spec.ErrConflict
	}
	// Incomplete or malformed Drafts remain editable. Any readable metadata.name
	// must still agree with the Policy's identity, including before full validation.
	name, _ := specdomain.Name(yaml)
	if err = bindPolicySpecKey(ctx, tx, scope, &policy, name); err != nil {
		return spec.PolicyRevision{}, err
	}
	now := time.Now().Unix()
	inserted, err := tx.ExecContext(ctx, `INSERT INTO policy_revision(policy_revision_id,policy_id,status,yaml,updated_by,updated_at)
 SELECT $1,id,'DRAFT',$2,$3,$4 FROM policy WHERE policy_id=$5
 AND NOT EXISTS(SELECT 1 FROM policy_revision WHERE policy_id=policy.id AND status='DRAFT')`, opaque.RandomToken("rev_"), yaml, scope.ActorID, now, id)
	if err != nil {
		return spec.PolicyRevision{}, specError(err)
	}
	count, err := inserted.RowsAffected()
	if err != nil {
		return spec.PolicyRevision{}, specError(err)
	}
	if count == 0 {
		_, err = tx.ExecContext(ctx, `UPDATE policy_revision SET yaml=$1,canonical_json=NULL,digest=NULL,updated_by=$2,updated_at=$3
 WHERE policy_id=(SELECT id FROM policy WHERE policy_id=$4) AND status='DRAFT'`, yaml, scope.ActorID, now, id)
		if err != nil {
			return spec.PolicyRevision{}, specError(err)
		}
	}
	draft, err := draftByPolicy(ctx, tx, scope, id)
	if err != nil {
		return spec.PolicyRevision{}, err
	}
	if err = specAudit(ctx, tx, scope, "policy_draft_saved", "revision", draft.ID); err != nil {
		return spec.PolicyRevision{}, err
	}
	if err = tx.Commit(); err != nil {
		return spec.PolicyRevision{}, specError(err)
	}
	return draft, nil
}
func (s *Specs) CommitPolicyRevision(ctx context.Context, scope spec.Scope, input spec.PublicationInput) (spec.PolicyRevision, bool, error) {
	if input.Now == nil {
		return spec.PolicyRevision{}, false, spec.ErrInvalid
	}
	policyID, yaml, key := input.PolicyID, input.YAML, input.IdempotencyKey
	operation, requestDigest := input.OperationType, input.RequestDigest
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return spec.PolicyRevision{}, false, specError(err)
	}
	defer tx.Rollback()
	if err = lockSpecProject(ctx, tx, scope); err != nil {
		return spec.PolicyRevision{}, false, err
	}
	p, err := lockMaintainedPolicy(ctx, tx, scope, policyID)
	if err != nil {
		return spec.PolicyRevision{}, false, err
	}
	if digest, id, err := recordByKey(ctx, tx, scope, operation, key); err == nil {
		if digest != requestDigest {
			return spec.PolicyRevision{}, false, spec.ErrConflict
		}
		r, err := revisionByID(ctx, tx, scope, id)
		return r, false, err
	} else if !errors.Is(err, spec.ErrNotFound) {
		return spec.PolicyRevision{}, false, err
	}
	if p.LifecycleState != "ACTIVE" {
		return spec.PolicyRevision{}, false, spec.ErrConflict
	}
	name, err := specdomain.Name(yaml)
	if err != nil {
		return spec.PolicyRevision{}, false, err
	}
	if err = bindPolicySpecKey(ctx, tx, scope, &p, name); err != nil {
		return spec.PolicyRevision{}, false, err
	}
	// For Draft publication, yaml is the exact content the caller reviewed.
	// Compare under the Policy lock so another session cannot change what is published.
	var draft spec.PolicyRevision
	if input.FromDraft {
		draft, err = draftByPolicy(ctx, tx, scope, policyID)
		if errors.Is(err, spec.ErrNotFound) {
			return spec.PolicyRevision{}, false, spec.ErrConflict
		}
		if err != nil {
			return spec.PolicyRevision{}, false, err
		}
		if draft.YAML != yaml {
			return spec.PolicyRevision{}, false, spec.ErrConflict
		}
	}
	var last int64
	if err = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(revision_number),0) FROM policy_revision WHERE policy_id=(SELECT id FROM policy WHERE policy_id=$1) AND status='PUBLISHED'", policyID).Scan(&last); err != nil {
		return spec.PolicyRevision{}, false, specError(err)
	}
	number, now := last+1, input.Now()
	revision := spec.PolicyRevision{ID: opaque.RandomToken("rev_"), PolicyID: policyID, RevisionNumber: &number, YAML: yaml, Metadata: input.Metadata, Digest: &input.SpecDigest, PublishedAt: &now, PublishedBy: &scope.ActorID, Status: "PUBLISHED", UpdatedBy: scope.ActorID, UpdatedAt: now}
	var metadata any
	if len(input.Metadata) > 0 {
		metadata = string(input.Metadata)
	}
	var result sql.Result
	if input.FromDraft {
		revision.ID = draft.ID
		result, err = tx.ExecContext(ctx, `UPDATE policy_revision SET status='PUBLISHED',revision_number=$1,canonical_json=$2,digest=$3,
 published_at=$4,published_by=$5,updated_at=$6,updated_by=$7,metadata_json=$8
 WHERE policy_revision_id=$9 AND status='DRAFT'`, revision.RevisionNumber, input.CanonicalJSON, input.SpecDigest, revision.PublishedAt, scope.ActorID, revision.UpdatedAt, revision.UpdatedBy, metadata, revision.ID)
	} else {
		result, err = tx.ExecContext(ctx, `INSERT INTO policy_revision(policy_revision_id,policy_id,status,revision_number,yaml,canonical_json,digest,published_at,published_by,updated_at,updated_by,metadata_json)
 VALUES($1,(SELECT id FROM policy WHERE policy_id=$2),'PUBLISHED',$3,$4,$5,$6,$7,$8,$9,$10,$11)`, revision.ID, policyID, revision.RevisionNumber, yaml, input.CanonicalJSON, input.SpecDigest, revision.PublishedAt, scope.ActorID, revision.UpdatedAt, revision.UpdatedBy, metadata)
	}
	if err != nil {
		return spec.PolicyRevision{}, false, specError(err)
	}
	if input.FromDraft {
		count, err := result.RowsAffected()
		if err != nil {
			return spec.PolicyRevision{}, false, specError(err)
		}
		if count != 1 {
			return spec.PolicyRevision{}, false, spec.ErrConflict
		}
	}
	if err = insertRecord(ctx, tx, scope, operation, key, requestDigest, "revision", revision.ID, *revision.PublishedAt); err != nil {
		return spec.PolicyRevision{}, false, err
	}
	if err = specAudit(ctx, tx, scope, "policy_revision_published", "revision", revision.ID); err != nil {
		return spec.PolicyRevision{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return spec.PolicyRevision{}, false, specError(err)
	}
	return revision, true, nil
}
func (s *Specs) ActivatePolicyRevision(ctx context.Context, scope spec.Scope, policyID, revisionID string, expected *string) (spec.Policy, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return spec.Policy{}, specError(err)
	}
	defer tx.Rollback()
	if err = lockSpecProject(ctx, tx, scope); err != nil {
		return spec.Policy{}, err
	}
	p, err := lockMaintainedPolicy(ctx, tx, scope, policyID)
	if err != nil {
		return spec.Policy{}, err
	}
	if p.LifecycleState != "ACTIVE" || !sameOptionalID(p.ActiveRevisionID, expected) {
		return spec.Policy{}, spec.ErrConflict
	}
	r, err := revisionByID(ctx, tx, scope, revisionID)
	if err != nil {
		return spec.Policy{}, err
	}
	if r.PolicyID != policyID {
		return spec.Policy{}, spec.ErrNotFound
	}
	if r.Status != "PUBLISHED" {
		return spec.Policy{}, spec.ErrConflict
	}
	name, err := specdomain.Name(r.YAML)
	if err != nil {
		return spec.Policy{}, err
	}
	if err = bindPolicySpecKey(ctx, tx, scope, &p, name); err != nil {
		return spec.Policy{}, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE policy SET active_revision_id=(SELECT id FROM policy_revision WHERE policy_revision_id=$1 AND status='PUBLISHED') WHERE policy_id=$2", revisionID, policyID); err != nil {
		return spec.Policy{}, specError(err)
	}
	if err = specAudit(ctx, tx, scope, "policy_revision_activated", "policy", policyID); err != nil {
		return spec.Policy{}, err
	}
	if err = tx.Commit(); err != nil {
		return spec.Policy{}, specError(err)
	}
	p.ActiveRevisionID = &revisionID
	return p, nil
}
func sameOptionalID(a, b *string) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}
func (s *Specs) SetDefaultPolicy(ctx context.Context, scope spec.Scope, sceneKey, policyID string, expected *string) (spec.SceneModel, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return spec.SceneModel{}, specError(err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "UPDATE scene SET name=name WHERE workspace_id=(SELECT id FROM workspace WHERE workspace_id=$1) AND project_id=(SELECT id FROM project WHERE project_id=$2) AND scene_key=$3", scope.WorkspaceID, scope.ProjectID, sceneKey)
	if err != nil {
		return spec.SceneModel{}, specError(err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return spec.SceneModel{}, specError(err)
	}
	if n == 0 {
		return spec.SceneModel{}, spec.ErrNotFound
	}
	scene, err := scanSceneModel(tx.QueryRowContext(ctx, "SELECT "+sceneModelColumns+" FROM scene WHERE workspace_id=(SELECT id FROM workspace WHERE workspace_id=$1) AND project_id=(SELECT id FROM project WHERE project_id=$2) AND scene_key=$3", scope.WorkspaceID, scope.ProjectID, sceneKey))
	if err != nil {
		return spec.SceneModel{}, err
	}
	if !sameOptionalID(scene.DefaultPolicyID, expected) {
		return spec.SceneModel{}, spec.ErrConflict
	}
	p, err := policyByID(ctx, tx, scope, policyID)
	if err != nil {
		return spec.SceneModel{}, err
	}
	if p.SceneKey != sceneKey || p.LifecycleState != "ACTIVE" || p.ActiveRevisionID == nil {
		return spec.SceneModel{}, spec.ErrConflict
	}
	if _, err = tx.ExecContext(ctx, "UPDATE scene SET default_policy_id=(SELECT id FROM policy WHERE policy_id=$1) WHERE workspace_id=(SELECT id FROM workspace WHERE workspace_id=$2) AND project_id=(SELECT id FROM project WHERE project_id=$3) AND scene_key=$4", policyID, scope.WorkspaceID, scope.ProjectID, sceneKey); err != nil {
		return spec.SceneModel{}, specError(err)
	}
	if err = specAudit(ctx, tx, scope, "scene_default_policy_changed", "scene", sceneKey); err != nil {
		return spec.SceneModel{}, err
	}
	if err = tx.Commit(); err != nil {
		return spec.SceneModel{}, specError(err)
	}
	scene.DefaultPolicyID = &policyID
	return scene, nil
}
func (s *Specs) SetSceneTTL(ctx context.Context, scope spec.Scope, sceneKey string, ttl int64) (spec.SceneModel, error) {
	return s.UpdateScene(ctx, scope, sceneKey, &ttl, nil)
}

func scenePluginsJSON(p *resource.Plugins) any {
	if p == nil || (p.ChangeSet == nil && p.BindingCandidates == nil) {
		return nil
	}
	data, _ := json.Marshal(p)
	return string(data)
}

func (s *Specs) UpdateScene(ctx context.Context, scope spec.Scope, sceneKey string, ttl *int64, plugins *resource.Plugins) (spec.SceneModel, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return spec.SceneModel{}, specError(err)
	}
	defer tx.Rollback()
	const where = " WHERE workspace_id=(SELECT id FROM workspace WHERE workspace_id=$2) AND project_id=(SELECT id FROM project WHERE project_id=$3) AND scene_key=$4"
	if ttl != nil {
		if _, err = tx.ExecContext(ctx, "UPDATE scene SET run_ttl_seconds=$1"+where, *ttl, scope.WorkspaceID, scope.ProjectID, sceneKey); err != nil {
			return spec.SceneModel{}, specError(err)
		}
		if err = specAudit(ctx, tx, scope, "scene_ttl_changed", "scene", sceneKey); err != nil {
			return spec.SceneModel{}, err
		}
	}
	if plugins != nil {
		if _, err = tx.ExecContext(ctx, "UPDATE scene SET plugins_json=$1"+where, scenePluginsJSON(plugins), scope.WorkspaceID, scope.ProjectID, sceneKey); err != nil {
			return spec.SceneModel{}, specError(err)
		}
		if err = specAudit(ctx, tx, scope, "scene_plugins_changed", "scene", sceneKey); err != nil {
			return spec.SceneModel{}, err
		}
	}
	scene, err := scanSceneModel(tx.QueryRowContext(ctx, "SELECT "+sceneModelColumns+" FROM scene WHERE workspace_id=(SELECT id FROM workspace WHERE workspace_id=$1) AND project_id=(SELECT id FROM project WHERE project_id=$2) AND scene_key=$3", scope.WorkspaceID, scope.ProjectID, sceneKey))
	if err != nil {
		return spec.SceneModel{}, err
	}
	if err = tx.Commit(); err != nil {
		return spec.SceneModel{}, specError(err)
	}
	return scene, nil
}
func (s *Specs) SetPolicyArchived(ctx context.Context, scope spec.Scope, id string, archived bool) (spec.Policy, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return spec.Policy{}, specError(err)
	}
	defer tx.Rollback()
	current, err := policyByID(ctx, tx, scope, id)
	if err != nil {
		return spec.Policy{}, err
	}
	// Default-policy changes and Run creation lock Scene first.
	if _, err = tx.ExecContext(ctx, "UPDATE scene SET name=name WHERE workspace_id=(SELECT id FROM workspace WHERE workspace_id=$1) AND project_id=(SELECT id FROM project WHERE project_id=$2) AND scene_key=$3", scope.WorkspaceID, scope.ProjectID, current.SceneKey); err != nil {
		return spec.Policy{}, specError(err)
	}
	p, err := lockMaintainedPolicy(ctx, tx, scope, id)
	if err != nil {
		return spec.Policy{}, err
	}
	state := "ACTIVE"
	event := "policy_unarchived"
	if archived {
		state = "ARCHIVED"
		event = "policy_archived"
	}
	if p.LifecycleState == state {
		return p, nil
	}
	if archived {
		var defaultID sql.NullString
		if err = tx.QueryRowContext(ctx, "SELECT (SELECT policy_id FROM policy WHERE id=scene.default_policy_id) FROM scene WHERE workspace_id=(SELECT id FROM workspace WHERE workspace_id=$1) AND project_id=(SELECT id FROM project WHERE project_id=$2) AND scene_key=$3", scope.WorkspaceID, scope.ProjectID, p.SceneKey).Scan(&defaultID); err != nil {
			return spec.Policy{}, specError(err)
		}
		if defaultID.Valid && defaultID.String == id {
			return spec.Policy{}, spec.ErrConflict
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE policy SET lifecycle_state=$1 WHERE policy_id=$2", state, id); err != nil {
		return spec.Policy{}, specError(err)
	}
	if err = specAudit(ctx, tx, scope, event, "policy", id); err != nil {
		return spec.Policy{}, err
	}
	if err = tx.Commit(); err != nil {
		return spec.Policy{}, specError(err)
	}
	p.LifecycleState = state
	return p, nil
}

// CommitSceneInitialization atomically creates the default Policy and its first active
// Revision. An existing uninitialized Scene may be initialized once.
func (s *Specs) CommitSceneInitialization(ctx context.Context, scope spec.Scope, input spec.InitializationInput) (spec.InitializedScene, bool, error) {
	if !spec.ValidActorID(scope.ActorID) {
		return spec.InitializedScene{}, false, spec.ErrInvalid
	}
	if input.Now == nil {
		return spec.InitializedScene{}, false, spec.ErrInvalid
	}
	sceneKey, sceneName, policyName, yaml, key := input.SceneKey, input.SceneName, input.PolicyName, input.YAML, input.IdempotencyKey
	canonical, specDigest, requestDigest := input.CanonicalJSON, input.SpecDigest, input.RequestDigest
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return spec.InitializedScene{}, false, specError(err)
	}
	defer tx.Rollback()
	if err = lockSpecProject(ctx, tx, scope); err != nil {
		return spec.InitializedScene{}, false, err
	}
	if digest, id, err := recordByKey(ctx, tx, scope, "scene:initialize", key); err == nil {
		if digest != requestDigest {
			return spec.InitializedScene{}, false, spec.ErrConflict
		}
		scene, err := scanSceneModel(tx.QueryRowContext(ctx, "SELECT "+sceneModelColumns+" FROM scene WHERE workspace_id=(SELECT id FROM workspace WHERE workspace_id=$1) AND project_id=(SELECT id FROM project WHERE project_id=$2) AND scene_key=$3", scope.WorkspaceID, scope.ProjectID, sceneKey))
		if err != nil {
			return spec.InitializedScene{}, false, err
		}
		policy, err := policyByID(ctx, tx, scope, id)
		if err != nil {
			return spec.InitializedScene{}, false, err
		}
		revision, err := scanPolicyRevision(tx.QueryRowContext(ctx, "SELECT "+revisionModelColumns+revisionModelFrom+" WHERE p.policy_id=$1 AND r.status='PUBLISHED' AND r.revision_number=1", id))
		if err != nil {
			return spec.InitializedScene{}, false, err
		}
		return spec.InitializedScene{Scene: scene, Policy: policy, Revision: revision}, false, nil
	} else if !errors.Is(err, spec.ErrNotFound) {
		return spec.InitializedScene{}, false, err
	}
	insertScene := "INSERT INTO scene(workspace_id,project_id,scene_key,name,created_by) VALUES((SELECT id FROM workspace WHERE workspace_id=$1),(SELECT id FROM project WHERE project_id=$2),$3,$4,$5)"
	insertScene += s.driver.conflictClause("workspace_id,project_id,scene_key")
	_, err = tx.ExecContext(ctx, insertScene, scope.WorkspaceID, scope.ProjectID, sceneKey, sceneName, scope.ActorID)
	if err != nil && !(s.driver.isDuplicate(err)) {
		return spec.InitializedScene{}, false, specError(err)
	}
	// Serialize different keys trying to initialize the same Scene.
	if _, err = tx.ExecContext(ctx, "UPDATE scene SET name=name WHERE workspace_id=(SELECT id FROM workspace WHERE workspace_id=$1) AND project_id=(SELECT id FROM project WHERE project_id=$2) AND scene_key=$3", scope.WorkspaceID, scope.ProjectID, sceneKey); err != nil {
		return spec.InitializedScene{}, false, specError(err)
	}
	scene, err := scanSceneModel(tx.QueryRowContext(ctx, "SELECT "+sceneModelColumns+" FROM scene WHERE workspace_id=(SELECT id FROM workspace WHERE workspace_id=$1) AND project_id=(SELECT id FROM project WHERE project_id=$2) AND scene_key=$3", scope.WorkspaceID, scope.ProjectID, sceneKey))
	if err != nil {
		return spec.InitializedScene{}, false, err
	}
	if scene.DefaultPolicyID != nil {
		return spec.InitializedScene{}, false, spec.ErrSceneAlreadyInitialized
	}
	specKey, err := specdomain.Name(yaml)
	if err != nil {
		return spec.InitializedScene{}, false, err
	}
	if err = checkSpecKeyAvailable(ctx, tx, scope, sceneKey, specKey, ""); err != nil {
		return spec.InitializedScene{}, false, err
	}
	now := input.Now()
	policy := spec.Policy{ID: opaque.RandomToken("pol_"), SceneKey: sceneKey, Name: policyName, SpecKey: specKey, LifecycleState: "ACTIVE", Maintainers: []string{scope.ActorID}}
	number := int64(1)
	revision := spec.PolicyRevision{ID: opaque.RandomToken("rev_"), PolicyID: policy.ID, RevisionNumber: &number, YAML: yaml, Digest: &specDigest, PublishedAt: &now, PublishedBy: &scope.ActorID, Status: "PUBLISHED", UpdatedBy: scope.ActorID, UpdatedAt: now}
	if _, err = tx.ExecContext(ctx, "INSERT INTO policy(policy_id,workspace_id,project_id,scene_key,name,spec_key,maintainers_json) VALUES($1,(SELECT id FROM workspace WHERE workspace_id=$2),(SELECT id FROM project WHERE project_id=$3),$4,$5,$6,$7)", policy.ID, scope.WorkspaceID, scope.ProjectID, sceneKey, policyName, specKey, creatorMaintainers(scope)); err != nil {
		return spec.InitializedScene{}, false, specError(err)
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO policy_revision(policy_revision_id,policy_id,status,revision_number,yaml,canonical_json,digest,published_at,published_by,updated_at,updated_by) VALUES($1,(SELECT id FROM policy WHERE policy_id=$2),'PUBLISHED',1,$3,$4,$5,$6,$7,$8,$9)", revision.ID, policy.ID, yaml, canonical, specDigest, now, scope.ActorID, revision.UpdatedAt, revision.UpdatedBy); err != nil {
		return spec.InitializedScene{}, false, specError(err)
	}
	if _, err = tx.ExecContext(ctx, "UPDATE policy SET active_revision_id=(SELECT id FROM policy_revision WHERE policy_revision_id=$1 AND status='PUBLISHED') WHERE policy_id=$2", revision.ID, policy.ID); err != nil {
		return spec.InitializedScene{}, false, specError(err)
	}
	if _, err = tx.ExecContext(ctx, "UPDATE scene SET default_policy_id=(SELECT id FROM policy WHERE policy_id=$1) WHERE workspace_id=(SELECT id FROM workspace WHERE workspace_id=$2) AND project_id=(SELECT id FROM project WHERE project_id=$3) AND scene_key=$4", policy.ID, scope.WorkspaceID, scope.ProjectID, sceneKey); err != nil {
		return spec.InitializedScene{}, false, specError(err)
	}
	if input.Plugins != nil {
		value := scenePluginsJSON(input.Plugins)
		if _, err = tx.ExecContext(ctx, "UPDATE scene SET plugins_json=$1 WHERE workspace_id=(SELECT id FROM workspace WHERE workspace_id=$2) AND project_id=(SELECT id FROM project WHERE project_id=$3) AND scene_key=$4", value, scope.WorkspaceID, scope.ProjectID, sceneKey); err != nil {
			return spec.InitializedScene{}, false, specError(err)
		}
		scene.Plugins = nil
		if value != nil {
			scene.Plugins = input.Plugins
		}
	}
	if err = insertRecord(ctx, tx, scope, "scene:initialize", key, requestDigest, "policy", policy.ID, now); err != nil {
		return spec.InitializedScene{}, false, err
	}
	if err = specAudit(ctx, tx, scope, "scene_initialized", "scene", sceneKey); err != nil {
		return spec.InitializedScene{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return spec.InitializedScene{}, false, specError(err)
	}
	scene.DefaultPolicyID = &policy.ID
	policy.ActiveRevisionID = &revision.ID
	return spec.InitializedScene{Scene: scene, Policy: policy, Revision: revision}, true, nil
}

// lockSpecProject serializes idempotent management writes before their first
// read. A key is scoped to the project and operation, not the target Policy.
// Always acquire this lock before Scene or Policy locks to keep one lock order.
func lockSpecProject(ctx context.Context, tx *sql.Tx, scope spec.Scope) error {
	result, err := tx.ExecContext(ctx, "UPDATE project SET name=name WHERE workspace_id=(SELECT id FROM workspace WHERE workspace_id=$1) AND project_id=$2", scope.WorkspaceID, scope.ProjectID)
	if err != nil {
		return specError(err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return specError(err)
	}
	if n == 0 {
		return spec.ErrNotFound
	}
	return nil
}
