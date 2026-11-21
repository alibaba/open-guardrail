-- +goose Up
ALTER TABLE policy ADD COLUMN maintainers_json TEXT;

-- Preserve recorded creators; unknown legacy owners stay NULL and cannot write.
UPDATE policy AS p SET maintainers_json = NULLIF(json_build_array(COALESCE((SELECT actor_id FROM audit_event WHERE workspace_id=p.workspace_id AND project_id=p.project_id AND resource_type='policy' AND resource_id=p.policy_id AND event_type='policy_created' AND actor_id<>'' ORDER BY id LIMIT 1),(SELECT published_by FROM policy_revision WHERE policy_id=p.id AND status='PUBLISHED' AND revision_number=1 AND published_by IS NOT NULL AND published_by<>'')))::text,'[null]');

-- +goose Down
ALTER TABLE policy DROP COLUMN maintainers_json;
