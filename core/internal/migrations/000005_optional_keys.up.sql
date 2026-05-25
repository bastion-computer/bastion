PRAGMA foreign_keys = OFF;
PRAGMA legacy_alter_table = ON;

DROP INDEX IF EXISTS idx_templates_created_at;
ALTER TABLE templates RENAME TO templates_required_key;

CREATE TABLE templates (
  id TEXT PRIMARY KEY,
  key TEXT,
  config TEXT NOT NULL,
  created_at TEXT NOT NULL
);

INSERT INTO templates (id, key, config, created_at)
SELECT id, NULLIF(key, ''), config, created_at FROM templates_required_key;

DROP TABLE templates_required_key;

CREATE INDEX idx_templates_created_at ON templates(created_at);
CREATE UNIQUE INDEX idx_templates_key ON templates(key) WHERE key IS NOT NULL;

DROP INDEX IF EXISTS idx_environments_created_at;

CREATE TABLE environments_optional_key (
  id TEXT PRIMARY KEY,
  key TEXT,
  status TEXT NOT NULL,
  template_id TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL DEFAULT '',
  last_error TEXT NOT NULL DEFAULT '',
  FOREIGN KEY (template_id) REFERENCES templates(id) ON DELETE RESTRICT
);

INSERT INTO environments_optional_key (id, key, status, template_id, created_at, updated_at, last_error)
SELECT id, NULL, status, template_id, created_at, updated_at, last_error FROM environments;

DROP TABLE environments;
ALTER TABLE environments_optional_key RENAME TO environments;

CREATE INDEX idx_environments_created_at ON environments(created_at);
CREATE UNIQUE INDEX idx_environments_key ON environments(key) WHERE key IS NOT NULL;

PRAGMA legacy_alter_table = OFF;
PRAGMA foreign_keys = ON;
