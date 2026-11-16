-- +goose Up
CREATE INDEX audit_event_project_id ON audit_event(workspace_id,project_id,id);

-- +goose Down
DROP INDEX audit_event_project_id;
