CREATE TABLE secrets (
  id TEXT PRIMARY KEY,
  key TEXT,
  value TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX idx_secrets_created_at ON secrets(created_at);
CREATE UNIQUE INDEX idx_secrets_key ON secrets(key) WHERE key IS NOT NULL;
