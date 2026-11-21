-- +goose Up
ALTER TABLE scene ADD COLUMN plugins_json TEXT;

-- +goose Down
ALTER TABLE scene DROP COLUMN plugins_json;
