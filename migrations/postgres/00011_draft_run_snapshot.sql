-- +goose Up
-- No Down: dropping frozen snapshots would invalidate existing Draft Runs.
ALTER TABLE guardrail_run ADD COLUMN spec_yaml TEXT;
ALTER TABLE guardrail_run ADD COLUMN spec_canonical_json TEXT;
ALTER TABLE guardrail_run ADD COLUMN spec_updated_by TEXT;
ALTER TABLE guardrail_run ADD COLUMN spec_updated_at BIGINT;
ALTER TABLE guardrail_run ADD COLUMN spec_metadata_json TEXT;
ALTER TABLE guardrail_run DROP CONSTRAINT guardrail_run_policy_revision_id_policy_id_spec_digest_fkey;
ALTER TABLE guardrail_run ADD CONSTRAINT guardrail_run_revision_fk
 FOREIGN KEY(policy_revision_id,policy_id) REFERENCES policy_revision(id,policy_id) DEFERRABLE INITIALLY DEFERRED;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION guardrail_run_published_fn() RETURNS trigger AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM policy_revision r WHERE r.id=NEW.policy_revision_id AND r.policy_id=NEW.policy_id
  AND ((r.status='PUBLISHED' AND r.digest=NEW.spec_digest) OR
       (r.status='DRAFT' AND NEW.spec_yaml IS NOT NULL AND NEW.spec_canonical_json IS NOT NULL)))
 THEN RAISE EXCEPTION 'run revision or snapshot invalid'; END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION guardrail_run_snapshot_freeze_fn() RETURNS trigger AS $$
BEGIN
 IF NEW.spec_yaml IS DISTINCT FROM OLD.spec_yaml OR NEW.spec_canonical_json IS DISTINCT FROM OLD.spec_canonical_json
  OR NEW.spec_updated_by IS DISTINCT FROM OLD.spec_updated_by OR NEW.spec_updated_at IS DISTINCT FROM OLD.spec_updated_at
  OR NEW.spec_metadata_json IS DISTINCT FROM OLD.spec_metadata_json
 THEN RAISE EXCEPTION 'run Spec snapshot is immutable'; END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
CREATE TRIGGER guardrail_run_snapshot_freeze BEFORE UPDATE ON guardrail_run FOR EACH ROW EXECUTE FUNCTION guardrail_run_snapshot_freeze_fn();
