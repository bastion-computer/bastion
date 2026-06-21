CREATE TABLE cluster_secrets (
  id TEXT PRIMARY KEY,
  namespace_id TEXT NOT NULL REFERENCES cluster_namespaces(id) ON DELETE CASCADE,
  key TEXT,
  value TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX idx_cluster_secrets_namespace_created_at ON cluster_secrets(namespace_id, created_at);
CREATE UNIQUE INDEX idx_cluster_secrets_namespace_key ON cluster_secrets(namespace_id, key) WHERE key IS NOT NULL;

CREATE TABLE cluster_templates (
  id TEXT PRIMARY KEY,
  namespace_id TEXT NOT NULL REFERENCES cluster_namespaces(id) ON DELETE CASCADE,
  key TEXT,
  config TEXT NOT NULL,
  archive_key TEXT NOT NULL,
  node_id TEXT REFERENCES cluster_nodes(id) ON DELETE SET NULL,
  derivative_template_id TEXT,
  created_at TEXT NOT NULL
);

CREATE INDEX idx_cluster_templates_namespace_created_at ON cluster_templates(namespace_id, created_at);
CREATE UNIQUE INDEX idx_cluster_templates_namespace_key ON cluster_templates(namespace_id, key) WHERE key IS NOT NULL;
CREATE UNIQUE INDEX idx_cluster_templates_archive_key ON cluster_templates(archive_key);

CREATE TABLE cluster_template_derivative_secrets (
  template_id TEXT NOT NULL REFERENCES cluster_templates(id) ON DELETE CASCADE,
  source_secret_id TEXT NOT NULL REFERENCES cluster_secrets(id) ON DELETE CASCADE,
  derivative_secret_id TEXT NOT NULL,
  position INTEGER NOT NULL,
  PRIMARY KEY (template_id, position)
);

CREATE INDEX idx_cluster_template_derivative_secrets_template_id ON cluster_template_derivative_secrets(template_id);
