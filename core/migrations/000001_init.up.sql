CREATE TABLE secrets (
  id TEXT PRIMARY KEY,
  key TEXT NOT NULL UNIQUE,
  env TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE secret_allowed_hosts (
  secret_id TEXT NOT NULL,
  host TEXT NOT NULL,
  PRIMARY KEY (secret_id, host),
  FOREIGN KEY (secret_id) REFERENCES secrets(id) ON DELETE CASCADE
);

CREATE TABLE templates (
  id TEXT PRIMARY KEY,
  key TEXT NOT NULL UNIQUE,
  config TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE sandboxes (
  id TEXT PRIMARY KEY,
  status TEXT NOT NULL,
  source_type TEXT NOT NULL,
  source_id TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE checkpoints (
  id TEXT PRIMARY KEY,
  key TEXT NOT NULL UNIQUE,
  source_sandbox_id TEXT NOT NULL,
  status TEXT NOT NULL,
  created_at TEXT NOT NULL,
  FOREIGN KEY (source_sandbox_id) REFERENCES sandboxes(id) ON DELETE CASCADE
);

CREATE INDEX idx_secrets_created_at ON secrets(created_at);
CREATE INDEX idx_templates_created_at ON templates(created_at);
CREATE INDEX idx_sandboxes_created_at ON sandboxes(created_at);
CREATE INDEX idx_checkpoints_created_at ON checkpoints(created_at);
