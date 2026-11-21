-- +goose Up
ALTER TABLE policy_revision ADD COLUMN metadata_json TEXT;

-- +goose Down
ALTER TABLE policy_revision DROP COLUMN metadata_json;
