-- +goose Up
CREATE TABLE policy_binding (
 workspace_id BIGINT NOT NULL, project_id BIGINT NOT NULL, scene_key TEXT NOT NULL,
 resource_kind TEXT NOT NULL, resource_id TEXT NOT NULL,
 policy_id BIGINT NOT NULL, policy_revision_id BIGINT,
 PRIMARY KEY(workspace_id,project_id,scene_key,resource_kind,resource_id),
 FOREIGN KEY(policy_id,workspace_id,project_id,scene_key)
 REFERENCES policy(id,workspace_id,project_id,scene_key) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(policy_revision_id,policy_id) REFERENCES policy_revision(id,policy_id) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX policy_binding_policy ON policy_binding(policy_id);

-- +goose Down
DROP TABLE policy_binding;
