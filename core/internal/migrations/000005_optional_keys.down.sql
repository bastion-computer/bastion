PRAGMA foreign_keys = OFF;
PRAGMA legacy_alter_table = ON;

DROP INDEX IF EXISTS idx_templates_key;
DROP INDEX IF EXISTS idx_templates_created_at;
ALTER TABLE templates RENAME TO templates_optional_key;

CREATE TABLE templates (
  id TEXT PRIMARY KEY,
  key TEXT NOT NULL UNIQUE,
  config TEXT NOT NULL,
  created_at TEXT NOT NULL
);

INSERT INTO templates (id, key, config, created_at)
SELECT id, COALESCE(key, id), config, created_at FROM templates_optional_key;

DROP TABLE templates_optional_key;

CREATE INDEX idx_templates_created_at ON templates(created_at);

DROP INDEX IF EXISTS idx_environments_key;
DROP INDEX IF EXISTS idx_environments_created_at;

CREATE TABLE environments_required_key (
  id TEXT PRIMARY KEY,
  status TEXT NOT NULL,
  template_id TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL DEFAULT '',
  last_error TEXT NOT NULL DEFAULT '',
  FOREIGN KEY (template_id) REFERENCES templates(id) ON DELETE RESTRICT
);

INSERT INTO environments_required_key (id, status, template_id, created_at, updated_at, last_error)
SELECT id, status, template_id, created_at, updated_at, last_error FROM environments;

DROP TABLE environments;
ALTER TABLE environments_required_key RENAME TO environments;

CREATE INDEX idx_environments_created_at ON environments(created_at);

PRAGMA legacy_alter_table = OFF;
PRAGMA foreign_keys = ON;
