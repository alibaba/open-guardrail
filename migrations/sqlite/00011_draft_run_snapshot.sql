-- +goose Up
-- No Down: dropping frozen snapshots would invalidate existing Draft Runs.
CREATE TABLE guardrail_run_new (
 id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
 run_id TEXT NOT NULL UNIQUE, workspace_id BIGINT NOT NULL, project_id BIGINT NOT NULL,
 scene_key TEXT NOT NULL, policy_id BIGINT NOT NULL, policy_revision_id BIGINT NOT NULL,
 spec_digest TEXT NOT NULL CHECK(length(spec_digest)=64 AND spec_digest NOT GLOB '*[^a-f0-9]*'),
 status TEXT NOT NULL DEFAULT 'OPEN' CHECK(status IN ('OPEN','FINISHED','EXPIRED')),
 change_set_json TEXT NOT NULL, change_set_digest TEXT NOT NULL CHECK(length(change_set_digest)=64 AND change_set_digest NOT GLOB '*[^a-f0-9]*'),
 idempotency_key TEXT NOT NULL, request_digest TEXT NOT NULL CHECK(length(request_digest)=64 AND request_digest NOT GLOB '*[^a-f0-9]*'),
 run_ttl_seconds BIGINT NOT NULL CHECK(run_ttl_seconds BETWEEN 1 AND 604800),
 deadline_at BIGINT NOT NULL, decision_ttl_seconds BIGINT NOT NULL CHECK(decision_ttl_seconds>0),
 latest_submission_no BIGINT NOT NULL DEFAULT 0 CHECK(latest_submission_no>=0),
 final_submission_id BIGINT, created_by TEXT NOT NULL, created_at BIGINT NOT NULL,
 finished_at BIGINT, expired_at BIGINT, reason_code TEXT,
 result_verdict TEXT CHECK(result_verdict IN ('PASS','BLOCKED','UNKNOWN')),
 decision_verdict TEXT CHECK(decision_verdict IN ('ALLOW','DENY')), decision_expires_at BIGINT,
 selection_json TEXT, business_id TEXT, spec_yaml TEXT, spec_canonical_json TEXT,
 spec_updated_by TEXT, spec_updated_at BIGINT, spec_metadata_json TEXT,
 UNIQUE(workspace_id,project_id,idempotency_key), CHECK(deadline_at=created_at+run_ttl_seconds),
 CHECK((status='OPEN' AND finished_at IS NULL AND expired_at IS NULL AND final_submission_id IS NULL
 AND reason_code IS NULL AND result_verdict IS NULL AND decision_verdict IS NULL AND decision_expires_at IS NULL)
 OR (status='FINISHED' AND finished_at IS NOT NULL AND expired_at IS NULL AND final_submission_id IS NOT NULL
 AND reason_code IS NULL AND result_verdict IS NOT NULL AND decision_verdict IS NOT NULL
 AND decision_expires_at IS NOT NULL AND decision_expires_at=finished_at+decision_ttl_seconds)
 OR (status='EXPIRED' AND finished_at IS NULL AND expired_at IS NOT NULL AND expired_at=deadline_at
 AND final_submission_id IS NULL AND reason_code IS NOT NULL AND reason_code='RUN_EXPIRED'
 AND result_verdict IS NOT NULL AND result_verdict='UNKNOWN' AND decision_verdict IS NOT NULL AND decision_verdict='DENY'
 AND decision_expires_at IS NOT NULL AND decision_expires_at=expired_at+decision_ttl_seconds)),
 FOREIGN KEY(project_id,workspace_id) REFERENCES project(id,workspace_id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(workspace_id,project_id,scene_key) REFERENCES scene(workspace_id,project_id,scene_key) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(policy_id,workspace_id,project_id,scene_key) REFERENCES policy(id,workspace_id,project_id,scene_key) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(policy_revision_id,policy_id) REFERENCES policy_revision(id,policy_id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(final_submission_id,id) REFERENCES submission(id,run_id) DEFERRABLE INITIALLY DEFERRED
);
INSERT INTO guardrail_run_new SELECT *,NULL,NULL,NULL,NULL,NULL FROM guardrail_run;
DROP TABLE guardrail_run;
ALTER TABLE guardrail_run_new RENAME TO guardrail_run;
CREATE INDEX guardrail_run_project_created ON guardrail_run(workspace_id,project_id,created_at DESC,run_id DESC);
CREATE UNIQUE INDEX uk_guardrail_run_business_id ON guardrail_run(workspace_id,project_id,scene_key,business_id) WHERE business_id IS NOT NULL;
-- +goose StatementBegin
CREATE TRIGGER guardrail_run_published BEFORE INSERT ON guardrail_run
WHEN NOT EXISTS(SELECT 1 FROM policy_revision r WHERE r.id=NEW.policy_revision_id AND r.policy_id=NEW.policy_id AND ((r.status='PUBLISHED' AND r.digest=NEW.spec_digest) OR (r.status='DRAFT' AND NEW.spec_yaml IS NOT NULL AND NEW.spec_canonical_json IS NOT NULL)))
BEGIN SELECT RAISE(ABORT,'run revision or snapshot invalid'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER guardrail_run_freeze BEFORE UPDATE ON guardrail_run
WHEN OLD.status IN ('FINISHED','EXPIRED') OR NEW.id IS NOT OLD.id OR NEW.run_id IS NOT OLD.run_id OR NEW.workspace_id IS NOT OLD.workspace_id OR NEW.project_id IS NOT OLD.project_id OR NEW.scene_key IS NOT OLD.scene_key OR NEW.policy_id IS NOT OLD.policy_id OR NEW.policy_revision_id IS NOT OLD.policy_revision_id OR NEW.spec_digest IS NOT OLD.spec_digest OR NEW.change_set_json IS NOT OLD.change_set_json OR NEW.change_set_digest IS NOT OLD.change_set_digest OR NEW.idempotency_key IS NOT OLD.idempotency_key OR NEW.request_digest IS NOT OLD.request_digest OR NEW.run_ttl_seconds IS NOT OLD.run_ttl_seconds OR NEW.deadline_at IS NOT OLD.deadline_at OR NEW.decision_ttl_seconds IS NOT OLD.decision_ttl_seconds OR NEW.created_by IS NOT OLD.created_by OR NEW.created_at IS NOT OLD.created_at
BEGIN SELECT RAISE(ABORT,'run snapshot and terminal state are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER guardrail_run_selection_freeze BEFORE UPDATE OF selection_json ON guardrail_run
WHEN NEW.selection_json IS NOT OLD.selection_json
BEGIN
 SELECT RAISE(ABORT,'Run selection is immutable');
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER guardrail_run_business_id_freeze BEFORE UPDATE ON guardrail_run
WHEN NEW.business_id IS NOT OLD.business_id
BEGIN SELECT RAISE(ABORT,'run business identity is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER guardrail_run_snapshot_freeze BEFORE UPDATE ON guardrail_run
WHEN NEW.spec_yaml IS NOT OLD.spec_yaml OR NEW.spec_canonical_json IS NOT OLD.spec_canonical_json
 OR NEW.spec_updated_by IS NOT OLD.spec_updated_by OR NEW.spec_updated_at IS NOT OLD.spec_updated_at
 OR NEW.spec_metadata_json IS NOT OLD.spec_metadata_json
BEGIN SELECT RAISE(ABORT,'run Spec snapshot is immutable'); END;
-- +goose StatementEnd
