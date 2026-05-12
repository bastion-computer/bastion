DROP INDEX IF EXISTS idx_checkpoints_created_at;
DROP INDEX IF EXISTS idx_sandboxes_created_at;
DROP INDEX IF EXISTS idx_templates_created_at;
DROP INDEX IF EXISTS idx_secrets_created_at;

DROP TABLE IF EXISTS checkpoints;
DROP TABLE IF EXISTS sandboxes;
DROP TABLE IF EXISTS templates;
DROP TABLE IF EXISTS secret_allowed_hosts;
DROP TABLE IF EXISTS secrets;
