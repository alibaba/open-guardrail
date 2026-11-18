-- +goose Up
ALTER TABLE guardrail_run ADD COLUMN business_id TEXT;
COMMENT ON COLUMN guardrail_run.business_id IS 'Optional caller-owned business correlation ID';
CREATE UNIQUE INDEX uk_guardrail_run_business_id ON guardrail_run(workspace_id,project_id,scene_key,business_id) WHERE business_id IS NOT NULL;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION guardrail_run_business_id_freeze_fn() RETURNS trigger AS $$
BEGIN
 IF NEW.business_id IS DISTINCT FROM OLD.business_id THEN RAISE EXCEPTION 'run business identity is immutable'; END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
CREATE TRIGGER guardrail_run_business_id_freeze BEFORE UPDATE ON guardrail_run FOR EACH ROW EXECUTE FUNCTION guardrail_run_business_id_freeze_fn();

-- +goose Down
DROP TRIGGER guardrail_run_business_id_freeze ON guardrail_run;
DROP FUNCTION guardrail_run_business_id_freeze_fn();
DROP INDEX uk_guardrail_run_business_id;
ALTER TABLE guardrail_run DROP COLUMN business_id;
