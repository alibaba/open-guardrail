-- +goose Up
-- NULL denotes an unknown historical creator. New Scenes write the authenticated actor.
ALTER TABLE scene ADD COLUMN created_by TEXT NULL;

-- +goose Down
ALTER TABLE scene DROP COLUMN created_by;
