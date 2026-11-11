-- +goose Up
ALTER TABLE guardrail_run ADD COLUMN selection_json TEXT;
ALTER TABLE audit_event ADD COLUMN details_json TEXT;

-- +goose StatementBegin
CREATE FUNCTION guardrail_run_selection_freeze_fn() RETURNS trigger AS $$
BEGIN
 IF NEW.selection_json IS DISTINCT FROM OLD.selection_json THEN
  RAISE EXCEPTION 'Run selection is immutable';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
CREATE TRIGGER guardrail_run_selection_freeze BEFORE UPDATE OF selection_json ON guardrail_run
 FOR EACH ROW EXECUTE FUNCTION guardrail_run_selection_freeze_fn();

-- +goose Down
DROP TRIGGER guardrail_run_selection_freeze ON guardrail_run;
DROP FUNCTION guardrail_run_selection_freeze_fn();
ALTER TABLE guardrail_run DROP COLUMN selection_json;
ALTER TABLE audit_event DROP COLUMN details_json;
