-- +goose Up
-- NULL is reserved for policies created before Spec keys were introduced.
ALTER TABLE policy ADD COLUMN spec_key TEXT;
CREATE UNIQUE INDEX uk_policy_scene_spec_key ON policy(workspace_id,project_id,scene_key,spec_key);

-- +goose Down
DROP INDEX uk_policy_scene_spec_key;
ALTER TABLE policy DROP COLUMN spec_key;
