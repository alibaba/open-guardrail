-- +goose Up
ALTER TABLE guardrail_run ADD COLUMN business_id TEXT;
CREATE UNIQUE INDEX uk_guardrail_run_business_id ON guardrail_run(workspace_id,project_id,scene_key,business_id) WHERE business_id IS NOT NULL;
-- +goose StatementBegin
CREATE TRIGGER guardrail_run_business_id_freeze BEFORE UPDATE ON guardrail_run
WHEN NEW.business_id IS NOT OLD.business_id
BEGIN SELECT RAISE(ABORT,'run business identity is immutable'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER guardrail_run_business_id_freeze;
DROP INDEX uk_guardrail_run_business_id;
ALTER TABLE guardrail_run DROP COLUMN business_id;
