DROP INDEX IF EXISTS idx_environment_tags_tag;
DROP INDEX IF EXISTS idx_environment_vms_state;
DROP INDEX IF EXISTS idx_environments_key;
DROP INDEX IF EXISTS idx_environments_created_at;
DROP INDEX IF EXISTS idx_templates_key;
DROP INDEX IF EXISTS idx_templates_created_at;

DROP TABLE IF EXISTS environment_tags;
DROP TABLE IF EXISTS environment_vms;
DROP TABLE IF EXISTS environments;
DROP TABLE IF EXISTS templates;
