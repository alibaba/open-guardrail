package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/alibaba/open-guardrail/internal/application/identity"
	runapp "github.com/alibaba/open-guardrail/internal/application/run"
	"github.com/alibaba/open-guardrail/internal/application/spec"
	rundomain "github.com/alibaba/open-guardrail/internal/domain/run"
	specdomain "github.com/alibaba/open-guardrail/internal/domain/spec"
)

const policyRunColumns = "gr.run_id,gr.scene_key,p.policy_id,pr.policy_revision_id,gr.spec_digest,gr.status,gr.change_set_json,gr.change_set_digest,gr.run_ttl_seconds,gr.deadline_at,gr.latest_submission_no,fs.submission_id,gr.created_at,gr.finished_at,gr.expired_at,gr.result_verdict,gr.decision_verdict,gr.decision_expires_at"
const policyRunFrom = " FROM guardrail_run gr JOIN policy p ON p.id=gr.policy_id JOIN policy_revision pr ON pr.id=gr.policy_revision_id LEFT JOIN submission fs ON fs.id=gr.final_submission_id"

func scanPolicyRun(row scanner) (runapp.PolicyRun, error) {
	var run runapp.PolicyRun
	var changeSet string
	var finalID, result, decision sql.NullString
	var finished, expired, expiresAt sql.NullInt64
	err := row.Scan(&run.ID, &run.SceneKey, &run.PolicyID, &run.PolicyRevisionID, &run.SpecDigest, &run.Status, &changeSet, &run.ChangeSetDigest, &run.RunTTLSeconds, &run.DeadlineAt, &run.LatestSubmissionNo, &finalID, &run.CreatedAt, &finished, &expired, &result, &decision, &expiresAt)
	if err != nil {
		return runapp.PolicyRun{}, runError(err)
	}
	run.ChangeSetJSON = json.RawMessage(changeSet)
	if finalID.Valid {
		run.FinalSubmissionID = &finalID.String
	}
	if finished.Valid {
		run.FinishedAt = &finished.Int64
	}
	if expired.Valid {
		run.ExpiredAt = &expired.Int64
	}
	if run.Status != "OPEN" {
		if !result.Valid || !decision.Valid || !expiresAt.Valid {
			return runapp.PolicyRun{}, runapp.ErrUnavailable
		}
		run.Result, run.Decision, run.DecisionExpiresAt = &result.String, &decision.String, &expiresAt.Int64
	}
	return run, nil
}

func scanPolicyRunSummary(row scanner) (runapp.PolicyRunSummary, error) {
	var run runapp.PolicyRunSummary
	var finalID, result, decision sql.NullString
	var finished, expired, expiresAt sql.NullInt64
	err := row.Scan(&run.ID, &run.SceneKey, &run.PolicyID, &run.PolicyRevisionID, &run.SpecDigest, &run.Status, &run.ChangeSetDigest, &run.RunTTLSeconds, &run.DeadlineAt, &run.LatestSubmissionNo, &finalID, &run.CreatedAt, &finished, &expired, &result, &decision, &expiresAt)
	if err != nil {
		return runapp.PolicyRunSummary{}, runError(err)
	}
	if finalID.Valid {
		run.FinalSubmissionID = &finalID.String
	}
	if finished.Valid {
		run.FinishedAt = &finished.Int64
	}
	if expired.Valid {
		run.ExpiredAt = &expired.Int64
	}
	if run.Status != "OPEN" {
		if !result.Valid || !decision.Valid || !expiresAt.Valid {
			return runapp.PolicyRunSummary{}, runapp.ErrUnavailable
		}
		run.Result, run.Decision, run.DecisionExpiresAt = &result.String, &decision.String, &expiresAt.Int64
	}
	return run, nil
}
func policyRunByID(ctx context.Context, q rowQueryer, scope runapp.Scope, id string) (runapp.PolicyRun, error) {
	return scanPolicyRun(q.QueryRowContext(ctx, "SELECT "+policyRunColumns+policyRunFrom+" WHERE gr.workspace_id=(SELECT id FROM workspace WHERE workspace_id=$1) AND gr.project_id=(SELECT id FROM project WHERE project_id=$2) AND gr.run_id=$3", scope.WorkspaceID, scope.ProjectID, id))
}
func (s *Runs) GetPolicyRun(ctx context.Context, scope runapp.Scope, id string) (runapp.PolicyRun, error) {
	return policyRunByID(ctx, s.db, scope, id)
}
func findPolicyRunByKey(ctx context.Context, q rowQueryer, scope runapp.Scope, key string) (runapp.PolicyRun, string, error) {
	var digest, id string
	err := q.QueryRowContext(ctx, "SELECT run_id,request_digest FROM guardrail_run WHERE workspace_id=(SELECT id FROM workspace WHERE workspace_id=$1) AND project_id=(SELECT id FROM project WHERE project_id=$2) AND idempotency_key=$3", scope.WorkspaceID, scope.ProjectID, key).Scan(&id, &digest)
	if err != nil {
		return runapp.PolicyRun{}, "", runError(err)
	}
	run, err := policyRunByID(ctx, q, scope, id)
	return run, digest, err
}

// CreatePolicyRun freezes the selected revision and Scene TTL in one transaction.
func (s *Runs) CreatePolicyRun(ctx context.Context, scope runapp.Scope, input runapp.PolicyRunCreateInput) (runapp.PolicyRun, bool, error) {
	if input.PolicyRevisionID != "" && !input.CanSelectRevision {
		return runapp.PolicyRun{}, false, runapp.ErrUnauthorized
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return runapp.PolicyRun{}, false, runError(err)
	}
	defer tx.Rollback()
	if existing, digest, err := findPolicyRunByKey(ctx, tx, scope, input.IdempotencyKey); err == nil {
		if digest != input.RequestDigest {
			return runapp.PolicyRun{}, false, runapp.ErrIdempotencyConflict
		}
		_ = tx.Rollback()
		refreshed, refreshErr := s.RefreshPolicyRun(ctx, scope, existing.ID, input.Now)
		return refreshed, false, refreshErr
	} else if !errors.Is(err, runapp.ErrNotFound) {
		return runapp.PolicyRun{}, false, err
	}
	locked, err := tx.ExecContext(ctx, "UPDATE scene SET run_ttl_seconds=run_ttl_seconds WHERE workspace_id=(SELECT id FROM workspace WHERE workspace_id=$1) AND project_id=(SELECT id FROM project WHERE project_id=$2) AND scene_key=$3", scope.WorkspaceID, scope.ProjectID, input.SceneKey)
	if err != nil {
		return runapp.PolicyRun{}, false, runError(err)
	}
	n, err := locked.RowsAffected()
	if err != nil {
		return runapp.PolicyRun{}, false, runError(err)
	}
	if n == 0 {
		return runapp.PolicyRun{}, false, runapp.ErrNotFound
	}
	var policyID, revisionID, digest, canonical string
	var ttl int64
	if input.PolicyRevisionID == "" {
		err = tx.QueryRowContext(ctx, `SELECT p.policy_id,r.policy_revision_id,r.digest,r.canonical_json,sc.run_ttl_seconds
   FROM scene sc JOIN policy p ON p.id=sc.default_policy_id
   JOIN policy_revision r ON r.id=p.active_revision_id
   WHERE sc.workspace_id=(SELECT id FROM workspace WHERE workspace_id=$1) AND sc.project_id=(SELECT id FROM project WHERE project_id=$2) AND sc.scene_key=$3 AND p.lifecycle_state='ACTIVE' AND r.status='PUBLISHED'`, scope.WorkspaceID, scope.ProjectID, input.SceneKey).Scan(&policyID, &revisionID, &digest, &canonical, &ttl)
	} else {
		err = tx.QueryRowContext(ctx, `SELECT p.policy_id,r.policy_revision_id,r.digest,r.canonical_json,sc.run_ttl_seconds
   FROM scene sc JOIN policy p ON p.workspace_id=sc.workspace_id AND p.project_id=sc.project_id AND p.scene_key=sc.scene_key
   JOIN policy_revision r ON r.policy_id=p.id
   WHERE sc.workspace_id=(SELECT id FROM workspace WHERE workspace_id=$1) AND sc.project_id=(SELECT id FROM project WHERE project_id=$2) AND sc.scene_key=$3 AND r.policy_revision_id=$4 AND p.lifecycle_state='ACTIVE' AND r.status='PUBLISHED'`, scope.WorkspaceID, scope.ProjectID, input.SceneKey, input.PolicyRevisionID).Scan(&policyID, &revisionID, &digest, &canonical, &ttl)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return runapp.PolicyRun{}, false, runapp.ErrSpecNotPublished
	}
	if err != nil {
		return runapp.PolicyRun{}, false, runError(err)
	}
	// Activation locks Policy. Re-read the selected Revision after taking the
	// same lock so a concurrent activation cannot mix identity and content.
	if _, err = tx.ExecContext(ctx, "UPDATE policy SET lifecycle_state=lifecycle_state WHERE policy_id=$1 AND workspace_id=(SELECT id FROM workspace WHERE workspace_id=$2) AND project_id=(SELECT id FROM project WHERE project_id=$3)", policyID, scope.WorkspaceID, scope.ProjectID); err != nil {
		return runapp.PolicyRun{}, false, runError(err)
	}
	if input.PolicyRevisionID == "" {
		err = tx.QueryRowContext(ctx, `SELECT r.policy_revision_id,r.digest,r.canonical_json FROM policy p
		 JOIN policy_revision r ON r.id=p.active_revision_id
		 WHERE p.policy_id=$1 AND p.lifecycle_state='ACTIVE' AND r.status='PUBLISHED'`, policyID).Scan(&revisionID, &digest, &canonical)
		if errors.Is(err, sql.ErrNoRows) {
			return runapp.PolicyRun{}, false, runapp.ErrSpecNotPublished
		}
		if err != nil {
			return runapp.PolicyRun{}, false, runError(err)
		}
	} else {
		var state string
		if err = tx.QueryRowContext(ctx, "SELECT lifecycle_state FROM policy WHERE policy_id=$1", policyID).Scan(&state); err != nil {
			return runapp.PolicyRun{}, false, runError(err)
		}
		if state != "ACTIVE" {
			return runapp.PolicyRun{}, false, runapp.ErrSpecNotPublished
		}
	}
	decisionTTL, err := specdomain.ResolveValidForSeconds(canonical)
	if err != nil || ttl < 1 || ttl > 604800 || input.Now > 9223372036854775807-ttl-decisionTTL {
		return runapp.PolicyRun{}, false, runapp.ErrInvalid
	}
	run := runapp.PolicyRun{ID: identity.RandomToken("run_"), SceneKey: input.SceneKey, PolicyID: policyID, PolicyRevisionID: revisionID, SpecDigest: digest, Status: "OPEN", ChangeSetJSON: json.RawMessage(input.ChangeSetJSON), ChangeSetDigest: input.ChangeSetDigest, RunTTLSeconds: ttl, DeadlineAt: input.Now + ttl, CreatedAt: input.Now}
	insert := `INSERT INTO guardrail_run(run_id,workspace_id,project_id,scene_key,policy_id,policy_revision_id,spec_digest,status,change_set_json,change_set_digest,idempotency_key,request_digest,run_ttl_seconds,deadline_at,decision_ttl_seconds,created_by,created_at)
  VALUES($1,(SELECT id FROM workspace WHERE workspace_id=$2),(SELECT id FROM project WHERE project_id=$3),$4,(SELECT id FROM policy WHERE policy_id=$5),(SELECT id FROM policy_revision WHERE policy_revision_id=$6 AND status='PUBLISHED'),$7,'OPEN',$8,$9,$10,$11,$12,$13,$14,$15,$16)`
	insert += s.driver.conflictClause("workspace_id,project_id,idempotency_key")
	result, err := tx.ExecContext(ctx, insert, run.ID, scope.WorkspaceID, scope.ProjectID, run.SceneKey, run.PolicyID, run.PolicyRevisionID, run.SpecDigest, input.ChangeSetJSON, run.ChangeSetDigest, input.IdempotencyKey, input.RequestDigest, run.RunTTLSeconds, run.DeadlineAt, decisionTTL, scope.ActorID, run.CreatedAt)
	duplicate := s.driver.isDuplicate(err)
	if err != nil && !duplicate {
		return runapp.PolicyRun{}, false, runError(err)
	}
	n = 0
	if !duplicate {
		n, err = result.RowsAffected()
		if err != nil {
			return runapp.PolicyRun{}, false, runError(err)
		}
	}
	if n == 0 {
		existing, existingDigest, lookupErr := findPolicyRunByKey(ctx, tx, scope, input.IdempotencyKey)
		if lookupErr != nil {
			if duplicate && errors.Is(lookupErr, runapp.ErrNotFound) {
				// A different unique key (for example, the generated run ID)
				// may have caused the duplicate-key error.
				return runapp.PolicyRun{}, false, runError(err)
			}
			return runapp.PolicyRun{}, false, lookupErr
		}
		if existingDigest != input.RequestDigest {
			return runapp.PolicyRun{}, false, runapp.ErrIdempotencyConflict
		}
		_ = tx.Rollback()
		refreshed, refreshErr := s.RefreshPolicyRun(ctx, scope, existing.ID, input.Now)
		return refreshed, false, refreshErr
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO run_submission_credential(run_id,token_hash,created_at) VALUES((SELECT id FROM guardrail_run WHERE run_id=$1),$2,$3)", run.ID, input.SubmissionTokenHash, input.Now); err != nil {
		return runapp.PolicyRun{}, false, runError(err)
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO audit_event(event_type,actor_id,workspace_id,project_id,occurred_at,resource_type,resource_id) VALUES('run_created',$1,(SELECT id FROM workspace WHERE workspace_id=$2),(SELECT id FROM project WHERE project_id=$3),$4,'run',$5)", scope.ActorID, scope.WorkspaceID, scope.ProjectID, input.Now, run.ID); err != nil {
		return runapp.PolicyRun{}, false, runError(err)
	}
	if err = tx.Commit(); err != nil {
		return runapp.PolicyRun{}, false, runError(err)
	}
	return run, true, nil
}

func scanSnapshotSubmission(row scanner) (runapp.SnapshotSubmission, string, error) {
	var sub runapp.SnapshotSubmission
	var digest string
	err := row.Scan(&sub.ID, &sub.RunID, &sub.SubmissionNo, &sub.BaseSubmissionNo, &digest, &sub.Verdict, &sub.Summary, &sub.FinishRun, &sub.CreatedAt)
	if err != nil {
		return runapp.SnapshotSubmission{}, "", runError(err)
	}
	return sub, digest, nil
}
func submissionByKey(ctx context.Context, q rowQueryer, runID, key string) (runapp.SnapshotSubmission, string, error) {
	return scanSnapshotSubmission(q.QueryRowContext(ctx, "SELECT s.submission_id,r.run_id,s.submission_no,s.base_submission_no,s.request_digest,s.verdict,s.summary,s.finish_run,s.created_at FROM submission s JOIN guardrail_run r ON r.id=s.run_id WHERE r.run_id=$1 AND s.idempotency_key=$2", runID, key))
}

// SubmitSnapshot serializes credential rotation, submission numbering, expiry,
// and finalization on an OPEN Run row. Authenticated credentials are rechecked
// after acquiring that lock. Terminal Runs cannot rotate; their current
// credential is rechecked before resolving an exact replay.
func (s *Runs) SubmitSnapshot(ctx context.Context, scope runapp.Scope, runID string, input runapp.SnapshotSubmitInput) (runapp.SnapshotSubmission, bool, error) {
	if input.Now == nil {
		return runapp.SnapshotSubmission{}, false, runapp.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return runapp.SnapshotSubmission{}, false, runError(err)
	}
	defer tx.Rollback()
	locked, err := tx.ExecContext(ctx, "UPDATE guardrail_run SET latest_submission_no=latest_submission_no WHERE workspace_id=(SELECT id FROM workspace WHERE workspace_id=$1) AND project_id=(SELECT id FROM project WHERE project_id=$2) AND run_id=$3 AND status='OPEN'", scope.WorkspaceID, scope.ProjectID, runID)
	if err != nil {
		return runapp.SnapshotSubmission{}, false, runError(err)
	}
	n, err := locked.RowsAffected()
	if err != nil {
		return runapp.SnapshotSubmission{}, false, runError(err)
	}
	var current runapp.PolicyRun
	if n == 0 {
		current, err = policyRunByID(ctx, tx, scope, runID)
		if err != nil {
			return runapp.SnapshotSubmission{}, false, err
		}
		if current.Status == "OPEN" {
			return runapp.SnapshotSubmission{}, false, runapp.ErrUnavailable
		}
	}
	var credentialExists int
	err = tx.QueryRowContext(ctx, "SELECT 1 FROM run_submission_credential WHERE run_id=(SELECT id FROM guardrail_run WHERE run_id=$1) AND token_hash=$2", runID, scope.SubmissionCredentialHash).Scan(&credentialExists)
	if errors.Is(err, sql.ErrNoRows) {
		return runapp.SnapshotSubmission{}, false, runapp.ErrUnauthorized
	}
	if err != nil {
		return runapp.SnapshotSubmission{}, false, runError(err)
	}
	var canonical string
	err = tx.QueryRowContext(ctx, `SELECT pr.canonical_json FROM guardrail_run gr
  JOIN policy_revision pr ON pr.id=gr.policy_revision_id
  WHERE gr.workspace_id=(SELECT id FROM workspace WHERE workspace_id=$1) AND gr.project_id=(SELECT id FROM project WHERE project_id=$2) AND gr.run_id=$3`, scope.WorkspaceID, scope.ProjectID, runID).Scan(&canonical)
	if err != nil {
		return runapp.SnapshotSubmission{}, false, runError(err)
	}
	evaluation, err := rundomain.EvaluateSnapshot(canonical, input.Snapshot)
	if err != nil {
		return runapp.SnapshotSubmission{}, false, err
	}
	if prior, digest, err := submissionByKey(ctx, tx, runID, input.IdempotencyKey); err == nil {
		if digest != evaluation.RequestDigest {
			return runapp.SnapshotSubmission{}, false, runapp.ErrIdempotencyConflict
		}
		return prior, false, nil
	} else if !errors.Is(err, runapp.ErrNotFound) {
		return runapp.SnapshotSubmission{}, false, err
	}
	if n != 0 {
		current, err = policyRunByID(ctx, tx, scope, runID)
		if err != nil {
			return runapp.SnapshotSubmission{}, false, err
		}
	}
	if current.Status == "EXPIRED" {
		return runapp.SnapshotSubmission{}, false, runapp.ErrExpired
	}
	if current.Status != "OPEN" {
		return runapp.SnapshotSubmission{}, false, runapp.ErrCompleted
	}
	// The successful conditional UPDATE holds the OPEN Run row lock through
	// credential verification and the subsequent state transition.
	now := input.Now()
	if now >= current.DeadlineAt {
		if err = expirePolicyRun(ctx, tx, scope, current, now); err != nil {
			return runapp.SnapshotSubmission{}, false, err
		}
		if err = tx.Commit(); err != nil {
			return runapp.SnapshotSubmission{}, false, runError(err)
		}
		return runapp.SnapshotSubmission{}, false, runapp.ErrExpired
	}
	if evaluation.Snapshot.BaseSubmissionNo != current.LatestSubmissionNo {
		return runapp.SnapshotSubmission{}, false, runapp.ErrBaseSubmissionMismatch
	}
	sub := runapp.SnapshotSubmission{ID: identity.RandomToken("sub_"), RunID: runID, SubmissionNo: current.LatestSubmissionNo + 1, BaseSubmissionNo: current.LatestSubmissionNo, Verdict: evaluation.Verdict, Summary: evaluation.Snapshot.Summary, FinishRun: evaluation.Snapshot.FinishRun, CreatedAt: now}
	if _, err = tx.ExecContext(ctx, `INSERT INTO submission(submission_id,run_id,submission_no,base_submission_no,idempotency_key,request_digest,payload_json,verdict,summary,finish_run,created_by,created_at)
  VALUES($1,(SELECT id FROM guardrail_run WHERE run_id=$2),$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, sub.ID, runID, sub.SubmissionNo, sub.BaseSubmissionNo, input.IdempotencyKey, evaluation.RequestDigest, evaluation.CanonicalJSON, sub.Verdict, sub.Summary, sub.FinishRun, scope.ActorID, sub.CreatedAt); err != nil {
		return runapp.SnapshotSubmission{}, false, runError(err)
	}
	for i, check := range evaluation.Snapshot.Checks {
		for j, evidence := range check.Evidence {
			if _, err = tx.ExecContext(ctx, `INSERT INTO evidence(submission_id,run_id,check_id,requirement_id,observed_at,query_summary,result_summary,reference,evidence_digest)
    VALUES((SELECT id FROM submission WHERE submission_id=$1),(SELECT id FROM guardrail_run WHERE run_id=$2),$3,$4,$5,$6,$7,$8,$9)`, sub.ID, runID, check.CheckID, evidence.RequirementID, evidence.ObservedAt, evidence.QuerySummary, evidence.ResultSummary, evidence.Reference, evaluation.EvidenceDigests[i][j]); err != nil {
				return runapp.SnapshotSubmission{}, false, runError(err)
			}
		}
	}
	if sub.FinishRun {
		_, err = tx.ExecContext(ctx, `UPDATE guardrail_run SET latest_submission_no=$1,status='FINISHED',
 final_submission_id=(SELECT id FROM submission WHERE submission_id=$2),finished_at=$3,
 result_verdict=$4,decision_verdict=$5,decision_expires_at=$6+decision_ttl_seconds WHERE run_id=$7`, sub.SubmissionNo, sub.ID, now, sub.Verdict, evaluation.Decision, now, runID)
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE guardrail_run SET latest_submission_no=$1 WHERE run_id=$2", sub.SubmissionNo, runID)
	}
	if err != nil {
		return runapp.SnapshotSubmission{}, false, runError(err)
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO audit_event(event_type,actor_id,workspace_id,project_id,occurred_at,resource_type,resource_id) VALUES('submission_created',$1,(SELECT id FROM workspace WHERE workspace_id=$2),(SELECT id FROM project WHERE project_id=$3),$4,'submission',$5)", scope.ActorID, scope.WorkspaceID, scope.ProjectID, now, sub.ID); err != nil {
		return runapp.SnapshotSubmission{}, false, runError(err)
	}
	if err = tx.Commit(); err != nil {
		return runapp.SnapshotSubmission{}, false, runError(err)
	}
	return sub, true, nil
}

func expirePolicyRun(ctx context.Context, tx *sql.Tx, scope runapp.Scope, current runapp.PolicyRun, now int64) error {
	if current.Status != "OPEN" || now < current.DeadlineAt {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE guardrail_run SET status='EXPIRED',expired_at=deadline_at,reason_code='RUN_EXPIRED',
 result_verdict='UNKNOWN',decision_verdict='DENY',decision_expires_at=deadline_at+decision_ttl_seconds WHERE run_id=$1 AND status='OPEN'`, current.ID); err != nil {
		return runError(err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO audit_event(event_type,actor_id,workspace_id,project_id,occurred_at,resource_type,resource_id) VALUES('run_expired','system:run-expiry',(SELECT id FROM workspace WHERE workspace_id=$1),(SELECT id FROM project WHERE project_id=$2),$3,'run',$4)", scope.WorkspaceID, scope.ProjectID, now, current.ID); err != nil {
		return runError(err)
	}
	return nil
}
func (s *Runs) RefreshPolicyRun(ctx context.Context, scope runapp.Scope, id string, now int64) (runapp.PolicyRun, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return runapp.PolicyRun{}, runError(err)
	}
	defer tx.Rollback()
	current, err := refreshPolicyRunTx(ctx, tx, scope, id, now)
	if err != nil {
		return runapp.PolicyRun{}, err
	}
	if err = tx.Commit(); err != nil {
		return runapp.PolicyRun{}, runError(err)
	}
	return current, nil
}

func refreshPolicyRunTx(ctx context.Context, tx *sql.Tx, scope runapp.Scope, id string, now int64) (runapp.PolicyRun, error) {
	current, err := policyRunByID(ctx, tx, scope, id)
	if err != nil {
		return runapp.PolicyRun{}, err
	}
	if current.Status == "OPEN" && now >= current.DeadlineAt {
		locked, err := tx.ExecContext(ctx, "UPDATE guardrail_run SET latest_submission_no=latest_submission_no WHERE run_id=$1 AND status='OPEN'", id)
		if err != nil {
			return runapp.PolicyRun{}, runError(err)
		}
		n, err := locked.RowsAffected()
		if err != nil {
			return runapp.PolicyRun{}, runError(err)
		}
		current, err = policyRunByID(ctx, tx, scope, id)
		if err != nil {
			return runapp.PolicyRun{}, err
		}
		if n == 1 && current.Status == "OPEN" {
			if err = expirePolicyRun(ctx, tx, scope, current, now); err != nil {
				return runapp.PolicyRun{}, err
			}
			current, err = policyRunByID(ctx, tx, scope, id)
			if err != nil {
				return runapp.PolicyRun{}, err
			}
		}
	}
	return current, nil
}
func (s *Runs) RotatePolicySubmissionCredential(ctx context.Context, scope runapp.Scope, id, hash string, clock func() int64) (runapp.PolicyRun, error) {
	if clock == nil {
		return runapp.PolicyRun{}, runapp.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return runapp.PolicyRun{}, runError(err)
	}
	defer tx.Rollback()
	locked, err := tx.ExecContext(ctx, "UPDATE guardrail_run SET latest_submission_no=latest_submission_no WHERE workspace_id=(SELECT id FROM workspace WHERE workspace_id=$1) AND project_id=(SELECT id FROM project WHERE project_id=$2) AND run_id=$3 AND status='OPEN'", scope.WorkspaceID, scope.ProjectID, id)
	if err != nil {
		return runapp.PolicyRun{}, runError(err)
	}
	n, err := locked.RowsAffected()
	if err != nil {
		return runapp.PolicyRun{}, runError(err)
	}
	current, err := policyRunByID(ctx, tx, scope, id)
	if err != nil {
		return runapp.PolicyRun{}, err
	}
	if n == 0 {
		if current.Status == "EXPIRED" {
			return runapp.PolicyRun{}, runapp.ErrExpired
		}
		return runapp.PolicyRun{}, runapp.ErrCompleted
	}
	now := clock()
	if now >= current.DeadlineAt {
		if err = expirePolicyRun(ctx, tx, scope, current, now); err != nil {
			return runapp.PolicyRun{}, err
		}
		if err = tx.Commit(); err != nil {
			return runapp.PolicyRun{}, runError(err)
		}
		return runapp.PolicyRun{}, runapp.ErrExpired
	}
	if _, err = tx.ExecContext(ctx, "UPDATE run_submission_credential SET token_hash=$1,created_at=$2 WHERE run_id=(SELECT id FROM guardrail_run WHERE run_id=$3)", hash, now, id); err != nil {
		return runapp.PolicyRun{}, runError(err)
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO audit_event(event_type,actor_id,workspace_id,project_id,occurred_at,resource_type,resource_id) VALUES('run_submission_token_rotated',$1,(SELECT id FROM workspace WHERE workspace_id=$2),(SELECT id FROM project WHERE project_id=$3),$4,'run',$5)", scope.ActorID, scope.WorkspaceID, scope.ProjectID, now, id); err != nil {
		return runapp.PolicyRun{}, runError(err)
	}
	if err = tx.Commit(); err != nil {
		return runapp.PolicyRun{}, runError(err)
	}
	return current, nil
}

// ListPolicyRuns pages by (created_at,id); overdue OPEN rows are selected as
// EXPIRED and then persisted before a page is returned. Ordinary pages use
// one read query; overdue rows share one transaction for their transitions.
func (s *Runs) ListPolicyRuns(ctx context.Context, scope runapp.Scope, sceneKey, status string, limit int, cursorAt int64, cursorID string, now int64) ([]runapp.PolicyRunSummary, bool, error) {
	if limit < 1 || limit > 100 {
		return nil, false, runapp.ErrInvalid
	}
	// Use one argument per placeholder occurrence for positional-binding adapters.
	rows, err := s.db.QueryContext(ctx, `SELECT gr.run_id,gr.scene_key,p.policy_id,pr.policy_revision_id,gr.spec_digest,gr.status,
 gr.change_set_digest,gr.run_ttl_seconds,gr.deadline_at,gr.latest_submission_no,
 fs.submission_id,gr.created_at,gr.finished_at,gr.expired_at,gr.result_verdict,gr.decision_verdict,gr.decision_expires_at`+policyRunFrom+`
 WHERE gr.workspace_id=(SELECT id FROM workspace WHERE workspace_id=$1) AND gr.project_id=(SELECT id FROM project WHERE project_id=$2)
 AND ($3='' OR gr.scene_key=$4)
 AND ($5='' OR (CASE WHEN gr.status='OPEN' AND gr.deadline_at<=$6 THEN 'EXPIRED' ELSE gr.status END)=$7)
 AND ($8=0 OR gr.created_at<$9 OR (gr.created_at=$10 AND gr.run_id<$11))
 ORDER BY gr.created_at DESC,gr.run_id DESC LIMIT $12`, scope.WorkspaceID, scope.ProjectID, sceneKey, sceneKey, status, now, status, cursorAt, cursorAt, cursorAt, cursorID, limit+1)
	if err != nil {
		return nil, false, runError(err)
	}
	out := []runapp.PolicyRunSummary{}
	for rows.Next() {
		run, scanErr := scanPolicyRunSummary(rows)
		if scanErr != nil {
			rows.Close()
			return nil, false, scanErr
		}
		out = append(out, run)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, false, runError(err)
	}
	if err = rows.Close(); err != nil {
		return nil, false, runError(err)
	}
	more := len(out) > limit
	if more {
		out = out[:limit]
	}
	var overdue []int
	for i, run := range out {
		if run.Status == "OPEN" && now >= run.DeadlineAt {
			overdue = append(overdue, i)
		}
	}
	if len(overdue) == 0 {
		return out, more, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, runError(err)
	}
	defer tx.Rollback()
	for _, i := range overdue {
		run, err := refreshPolicyRunTx(ctx, tx, scope, out[i].ID, now)
		if err != nil {
			return nil, false, err
		}
		out[i] = run.Summary()
	}
	if err = tx.Commit(); err != nil {
		return nil, false, runError(err)
	}
	return out, more, nil
}

// GetRunPolicyRevision only exposes the immutable Revision selected by this Run.
func (s *Runs) GetRunPolicyRevision(ctx context.Context, scope runapp.Scope, runID string) (spec.PolicyRevision, error) {
	revision, err := scanPolicyRevision(s.db.QueryRowContext(ctx, `SELECT pr.policy_revision_id,p.policy_id,pr.revision_number,pr.yaml,pr.digest,pr.published_at,pr.published_by,pr.status,pr.updated_by,pr.updated_at
 FROM policy_revision pr JOIN policy p ON p.id=pr.policy_id JOIN guardrail_run gr ON gr.policy_revision_id=pr.id
 WHERE gr.workspace_id=(SELECT id FROM workspace WHERE workspace_id=$1) AND gr.project_id=(SELECT id FROM project WHERE project_id=$2) AND gr.run_id=$3`, scope.WorkspaceID, scope.ProjectID, runID))
	if errors.Is(err, spec.ErrNotFound) {
		return spec.PolicyRevision{}, runError(sql.ErrNoRows)
	}
	if err != nil {
		return spec.PolicyRevision{}, runError(err)
	}
	return revision, nil
}

func (s *Runs) GetRunSubmission(ctx context.Context, scope runapp.Scope, runID, submissionID string) (runapp.StoredSubmission, error) {
	var out runapp.StoredSubmission
	var payload string
	err := s.db.QueryRowContext(ctx, `SELECT sub.submission_id,gr.run_id,sub.submission_no,sub.base_submission_no,sub.verdict,sub.summary,sub.finish_run,sub.created_at,sub.payload_json
 FROM submission sub JOIN guardrail_run gr ON gr.id=sub.run_id
 WHERE gr.workspace_id=(SELECT id FROM workspace WHERE workspace_id=$1) AND gr.project_id=(SELECT id FROM project WHERE project_id=$2) AND gr.run_id=$3 AND sub.submission_id=$4`, scope.WorkspaceID, scope.ProjectID, runID, submissionID).Scan(&out.ID, &out.RunID, &out.SubmissionNo, &out.BaseSubmissionNo, &out.Verdict, &out.Summary, &out.FinishRun, &out.CreatedAt, &payload)
	if err != nil {
		return runapp.StoredSubmission{}, runError(err)
	}
	if err = json.Unmarshal([]byte(payload), &out.Snapshot); err != nil {
		return runapp.StoredSubmission{}, runError(err)
	}
	return out, nil
}

func (s *Runs) ListRunSubmissions(ctx context.Context, scope runapp.Scope, runID string, limit int, before int64) (runapp.SubmissionPage, error) {
	if limit < 1 || limit > 50 || before < 0 {
		return runapp.SubmissionPage{}, runapp.ErrInvalid
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, "SELECT 1 FROM guardrail_run WHERE workspace_id=(SELECT id FROM workspace WHERE workspace_id=$1) AND project_id=(SELECT id FROM project WHERE project_id=$2) AND run_id=$3", scope.WorkspaceID, scope.ProjectID, runID).Scan(&exists); err != nil {
		return runapp.SubmissionPage{}, runError(err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT sub.submission_id,gr.run_id,sub.submission_no,sub.base_submission_no,sub.verdict,sub.summary,sub.finish_run,sub.created_at
 FROM submission sub JOIN guardrail_run gr ON gr.id=sub.run_id
 WHERE gr.workspace_id=(SELECT id FROM workspace WHERE workspace_id=$1) AND gr.project_id=(SELECT id FROM project WHERE project_id=$2) AND gr.run_id=$3 AND ($4=0 OR sub.submission_no<$5)
 ORDER BY sub.submission_no DESC LIMIT $6`, scope.WorkspaceID, scope.ProjectID, runID, before, before, limit+1)
	if err != nil {
		return runapp.SubmissionPage{}, runError(err)
	}
	defer rows.Close()
	page := runapp.SubmissionPage{Items: []runapp.SnapshotSubmission{}}
	for rows.Next() {
		var sub runapp.SnapshotSubmission
		if err := rows.Scan(&sub.ID, &sub.RunID, &sub.SubmissionNo, &sub.BaseSubmissionNo, &sub.Verdict, &sub.Summary, &sub.FinishRun, &sub.CreatedAt); err != nil {
			return runapp.SubmissionPage{}, runError(err)
		}
		page.Items = append(page.Items, sub)
	}
	if err := rows.Err(); err != nil {
		return runapp.SubmissionPage{}, runError(err)
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		last := page.Items[len(page.Items)-1].SubmissionNo
		page.NextBefore = &last
	}
	return page, nil
}
