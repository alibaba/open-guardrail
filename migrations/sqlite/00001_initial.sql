-- +goose Up
CREATE TABLE identity_state (
 id INTEGER NOT NULL PRIMARY KEY CHECK(id=1),
 initialized INTEGER NOT NULL DEFAULT 0 CHECK(initialized IN (0,1)),
 bootstrap_hash TEXT NOT NULL DEFAULT '', bootstrap_expires BIGINT NOT NULL DEFAULT 0,
 login_attempts BIGINT NOT NULL DEFAULT 0, login_retry_at BIGINT NOT NULL DEFAULT 0,
 login_last_attempt BIGINT NOT NULL DEFAULT 0
) WITHOUT ROWID;
INSERT INTO identity_state(id) VALUES(1);
CREATE TABLE workspace (
 id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
 workspace_id TEXT NOT NULL UNIQUE, name TEXT NOT NULL
);
CREATE TABLE project (
 id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
 project_id TEXT NOT NULL UNIQUE, workspace_id BIGINT NOT NULL,
 name TEXT NOT NULL, UNIQUE(id,workspace_id),
 FOREIGN KEY(workspace_id) REFERENCES workspace(id) DEFERRABLE INITIALLY DEFERRED
);
CREATE TABLE local_user (
 id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
 user_id TEXT NOT NULL UNIQUE CHECK(user_id='admin'), username TEXT NOT NULL UNIQUE,
 workspace_id BIGINT NOT NULL, project_id BIGINT NOT NULL, password_hash TEXT NOT NULL,
 FOREIGN KEY(project_id,workspace_id) REFERENCES project(id,workspace_id) DEFERRABLE INITIALLY DEFERRED
);
CREATE TABLE admin_session (
 id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
 token_hash TEXT NOT NULL UNIQUE, user_id BIGINT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('browser','cli')), expires_at BIGINT NOT NULL,
 FOREIGN KEY(user_id) REFERENCES local_user(id) DEFERRABLE INITIALLY DEFERRED
);
CREATE TABLE project_api_key (
 id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
 api_key_id TEXT NOT NULL UNIQUE, token_hash TEXT NOT NULL UNIQUE,
 workspace_id BIGINT NOT NULL, project_id BIGINT NOT NULL,
 role TEXT NOT NULL CHECK(role IN ('RUN_ORCHESTRATOR','POLICY_PUBLISHER')),
 scopes_json TEXT NOT NULL DEFAULT '[]', created_at BIGINT NOT NULL,
 expires_at BIGINT NOT NULL, version BIGINT NOT NULL DEFAULT 1, revoked_at BIGINT,
 FOREIGN KEY(project_id,workspace_id) REFERENCES project(id,workspace_id) DEFERRABLE INITIALLY DEFERRED
);
CREATE TABLE audit_event (
 id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
 event_type TEXT NOT NULL, actor_id TEXT NOT NULL,
 workspace_id BIGINT, project_id BIGINT, occurred_at BIGINT NOT NULL,
 resource_type TEXT, resource_id TEXT
);
CREATE TABLE scene (
 id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
 workspace_id BIGINT NOT NULL, project_id BIGINT NOT NULL, scene_key TEXT NOT NULL,
 name TEXT NOT NULL, default_policy_id BIGINT,
 run_ttl_seconds BIGINT NOT NULL DEFAULT 86400 CHECK(run_ttl_seconds BETWEEN 1 AND 604800),
 UNIQUE(workspace_id,project_id,scene_key),
 FOREIGN KEY(project_id,workspace_id) REFERENCES project(id,workspace_id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(default_policy_id,workspace_id,project_id,scene_key)
 REFERENCES policy(id,workspace_id,project_id,scene_key) DEFERRABLE INITIALLY DEFERRED
);
CREATE TABLE policy (
 id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
 policy_id TEXT NOT NULL UNIQUE, workspace_id BIGINT NOT NULL,
 project_id BIGINT NOT NULL, scene_key TEXT NOT NULL, name TEXT NOT NULL,
 lifecycle_state TEXT NOT NULL DEFAULT 'ACTIVE' CHECK(lifecycle_state IN ('ACTIVE','ARCHIVED')),
 active_revision_id BIGINT,
 UNIQUE(id,workspace_id,project_id,scene_key),
 FOREIGN KEY(workspace_id,project_id,scene_key) REFERENCES scene(workspace_id,project_id,scene_key) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(active_revision_id,id) REFERENCES policy_revision(id,policy_id) DEFERRABLE INITIALLY DEFERRED
);
CREATE TABLE policy_revision (
 id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
 policy_revision_id TEXT NOT NULL UNIQUE, policy_id BIGINT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('DRAFT','PUBLISHED')), revision_number BIGINT,
 yaml TEXT NOT NULL, canonical_json TEXT, digest TEXT,
 updated_by TEXT NOT NULL, updated_at BIGINT NOT NULL, published_by TEXT, published_at BIGINT,
 UNIQUE(policy_id,revision_number), UNIQUE(id,policy_id), UNIQUE(id,policy_id,digest),
 CHECK((status='DRAFT' AND revision_number IS NULL AND canonical_json IS NULL AND digest IS NULL
 AND published_by IS NULL AND published_at IS NULL)
 OR (status='PUBLISHED' AND revision_number IS NOT NULL AND revision_number>0
 AND canonical_json IS NOT NULL AND digest IS NOT NULL AND length(digest)=64 AND digest NOT GLOB '*[^a-f0-9]*'
 AND published_by IS NOT NULL AND published_at IS NOT NULL)),
 FOREIGN KEY(policy_id) REFERENCES policy(id) DEFERRABLE INITIALLY DEFERRED
);
CREATE TABLE idempotency_record (
 id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
 workspace_id BIGINT NOT NULL, project_id BIGINT NOT NULL,
 operation_type TEXT NOT NULL, idempotency_key TEXT NOT NULL,
 request_digest TEXT NOT NULL CHECK(length(request_digest)=64 AND request_digest NOT GLOB '*[^a-f0-9]*'), resource_type TEXT NOT NULL,
 resource_id TEXT NOT NULL, created_at BIGINT NOT NULL,
 UNIQUE(workspace_id,project_id,operation_type,idempotency_key),
 FOREIGN KEY(project_id,workspace_id) REFERENCES project(id,workspace_id) DEFERRABLE INITIALLY DEFERRED
);
CREATE TABLE guardrail_run (
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
 FOREIGN KEY(policy_revision_id,policy_id,spec_digest) REFERENCES policy_revision(id,policy_id,digest) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(final_submission_id,id) REFERENCES submission(id,run_id) DEFERRABLE INITIALLY DEFERRED
);
CREATE TABLE submission (
 id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
 submission_id TEXT NOT NULL UNIQUE, run_id BIGINT NOT NULL,
 submission_no BIGINT NOT NULL CHECK(submission_no>0), base_submission_no BIGINT NOT NULL CHECK(base_submission_no>=0),
 idempotency_key TEXT NOT NULL, request_digest TEXT NOT NULL CHECK(length(request_digest)=64 AND request_digest NOT GLOB '*[^a-f0-9]*'), payload_json TEXT NOT NULL,
 verdict TEXT NOT NULL CHECK(verdict IN ('PASS','BLOCKED','UNKNOWN')), summary TEXT NOT NULL,
 finish_run INTEGER NOT NULL CHECK(finish_run IN (0,1)), created_by TEXT NOT NULL, created_at BIGINT NOT NULL,
 UNIQUE(run_id,submission_no), UNIQUE(run_id,idempotency_key), UNIQUE(id,run_id),
 CHECK(submission_no=base_submission_no+1),
 FOREIGN KEY(run_id) REFERENCES guardrail_run(id) DEFERRABLE INITIALLY DEFERRED
);
CREATE TABLE evidence (
 id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
 submission_id BIGINT NOT NULL, run_id BIGINT NOT NULL, check_id TEXT NOT NULL,
 requirement_id TEXT NOT NULL, observed_at TEXT NOT NULL, query_summary TEXT NOT NULL,
 result_summary TEXT NOT NULL, reference TEXT, evidence_digest TEXT NOT NULL CHECK(length(evidence_digest)=64 AND evidence_digest NOT GLOB '*[^a-f0-9]*'),
 UNIQUE(submission_id,check_id,requirement_id,evidence_digest),
 FOREIGN KEY(submission_id,run_id) REFERENCES submission(id,run_id) DEFERRABLE INITIALLY DEFERRED
);
CREATE TABLE run_submission_credential (
 id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
 run_id BIGINT NOT NULL UNIQUE, token_hash TEXT NOT NULL UNIQUE CHECK(length(token_hash)=64 AND token_hash NOT GLOB '*[^a-f0-9]*'), created_at BIGINT NOT NULL,
 FOREIGN KEY(run_id) REFERENCES guardrail_run(id) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX admin_session_user ON admin_session(user_id);
CREATE INDEX project_api_key_project_id ON project_api_key(workspace_id,project_id,api_key_id);
CREATE INDEX audit_event_time ON audit_event(occurred_at);
CREATE INDEX audit_event_resource ON audit_event(workspace_id,project_id,resource_type,resource_id);
CREATE INDEX policy_scene ON policy(workspace_id,project_id,scene_key);
CREATE INDEX policy_revision_history ON policy_revision(policy_id,revision_number DESC);
CREATE INDEX guardrail_run_project_created ON guardrail_run(workspace_id,project_id,created_at DESC,run_id DESC);
CREATE INDEX evidence_submission ON evidence(submission_id);
CREATE UNIQUE INDEX policy_revision_one_draft ON policy_revision(policy_id) WHERE status='DRAFT';
-- +goose StatementBegin
CREATE TRIGGER policy_revision_no_update BEFORE UPDATE ON policy_revision
WHEN OLD.status='PUBLISHED' OR NEW.id IS NOT OLD.id OR NEW.policy_revision_id IS NOT OLD.policy_revision_id OR NEW.policy_id IS NOT OLD.policy_id
BEGIN SELECT RAISE(ABORT,'published revisions and revision identity are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER policy_revision_no_delete BEFORE DELETE ON policy_revision
WHEN TRUE
BEGIN SELECT RAISE(ABORT,'policy revisions cannot be deleted'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER policy_active_published BEFORE UPDATE ON policy
WHEN NEW.active_revision_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM policy_revision r WHERE r.id=NEW.active_revision_id AND r.policy_id=NEW.id AND r.status='PUBLISHED')
BEGIN SELECT RAISE(ABORT,'active revision must be published and belong to policy'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER policy_insert_published BEFORE INSERT ON policy
WHEN NEW.active_revision_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM policy_revision r WHERE r.id=NEW.active_revision_id AND r.policy_id=NEW.id AND r.status='PUBLISHED')
BEGIN SELECT RAISE(ABORT,'active revision must be published and belong to policy'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER guardrail_run_published BEFORE INSERT ON guardrail_run
WHEN NOT EXISTS(SELECT 1 FROM policy_revision r WHERE r.id=NEW.policy_revision_id AND r.policy_id=NEW.policy_id AND r.status='PUBLISHED')
BEGIN SELECT RAISE(ABORT,'run revision must be published'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER guardrail_run_freeze BEFORE UPDATE ON guardrail_run
WHEN OLD.status IN ('FINISHED','EXPIRED') OR NEW.id IS NOT OLD.id OR NEW.run_id IS NOT OLD.run_id OR NEW.workspace_id IS NOT OLD.workspace_id OR NEW.project_id IS NOT OLD.project_id OR NEW.scene_key IS NOT OLD.scene_key OR NEW.policy_id IS NOT OLD.policy_id OR NEW.policy_revision_id IS NOT OLD.policy_revision_id OR NEW.spec_digest IS NOT OLD.spec_digest OR NEW.change_set_json IS NOT OLD.change_set_json OR NEW.change_set_digest IS NOT OLD.change_set_digest OR NEW.idempotency_key IS NOT OLD.idempotency_key OR NEW.request_digest IS NOT OLD.request_digest OR NEW.run_ttl_seconds IS NOT OLD.run_ttl_seconds OR NEW.deadline_at IS NOT OLD.deadline_at OR NEW.decision_ttl_seconds IS NOT OLD.decision_ttl_seconds OR NEW.created_by IS NOT OLD.created_by OR NEW.created_at IS NOT OLD.created_at
BEGIN SELECT RAISE(ABORT,'run snapshot and terminal state are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER submission_no_update BEFORE UPDATE ON submission
WHEN TRUE
BEGIN SELECT RAISE(ABORT,'submission is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER submission_no_delete BEFORE DELETE ON submission
WHEN TRUE
BEGIN SELECT RAISE(ABORT,'submission is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER evidence_no_update BEFORE UPDATE ON evidence
WHEN TRUE
BEGIN SELECT RAISE(ABORT,'evidence is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER evidence_no_delete BEFORE DELETE ON evidence
WHEN TRUE
BEGIN SELECT RAISE(ABORT,'evidence is immutable'); END;
-- +goose StatementEnd
