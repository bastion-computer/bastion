CREATE TABLE templates (
  id TEXT PRIMARY KEY,
  key TEXT NOT NULL UNIQUE,
  config TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE environments (
  id TEXT PRIMARY KEY,
  status TEXT NOT NULL,
  template_id TEXT NOT NULL,
  created_at TEXT NOT NULL,
  FOREIGN KEY (template_id) REFERENCES templates(id) ON DELETE RESTRICT
);

CREATE INDEX idx_templates_created_at ON templates(created_at);
CREATE INDEX idx_environments_created_at ON environments(created_at);
