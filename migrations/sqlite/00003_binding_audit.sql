-- +goose Up
ALTER TABLE guardrail_run ADD COLUMN selection_json TEXT;
ALTER TABLE audit_event ADD COLUMN details_json TEXT;

-- +goose StatementBegin
CREATE TRIGGER guardrail_run_selection_freeze BEFORE UPDATE OF selection_json ON guardrail_run
WHEN NEW.selection_json IS NOT OLD.selection_json
BEGIN
 SELECT RAISE(ABORT,'Run selection is immutable');
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER guardrail_run_selection_freeze;
ALTER TABLE guardrail_run DROP COLUMN selection_json;
ALTER TABLE audit_event DROP COLUMN details_json;
