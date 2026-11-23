-- +goose Up
ALTER TABLE policy ADD COLUMN spec_key VARCHAR(128);
COMMENT ON COLUMN policy.spec_key IS 'Immutable Spec identifier, unique within the Scene; NULL for legacy policies';
CREATE UNIQUE INDEX uk_policy_scene_spec_key ON policy(workspace_id,project_id,scene_key,spec_key);

-- +goose Down
DROP INDEX uk_policy_scene_spec_key;
ALTER TABLE policy DROP COLUMN spec_key;
