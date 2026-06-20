CREATE TABLE cluster_nodes (
  id TEXT PRIMARY KEY,
  key TEXT UNIQUE,
  api_url TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX idx_cluster_nodes_created_at ON cluster_nodes(created_at);

CREATE TABLE cluster_namespaces (
  id TEXT PRIMARY KEY,
  key TEXT UNIQUE,
  vcpu_limit BIGINT NOT NULL DEFAULT 0,
  memory_limit BIGINT NOT NULL DEFAULT 0,
  volume_limit BIGINT NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL
);

CREATE INDEX idx_cluster_namespaces_created_at ON cluster_namespaces(created_at);

CREATE TABLE cluster_secrets (
  namespace_id TEXT NOT NULL REFERENCES cluster_namespaces(id) ON DELETE RESTRICT,
  id TEXT PRIMARY KEY,
  key TEXT,
  value TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(namespace_id, key)
);

CREATE INDEX idx_cluster_secrets_namespace_created_at ON cluster_secrets(namespace_id, created_at);

CREATE TABLE cluster_templates (
  namespace_id TEXT NOT NULL REFERENCES cluster_namespaces(id) ON DELETE RESTRICT,
  id TEXT PRIMARY KEY,
  key TEXT,
  config TEXT NOT NULL,
  archive_key TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(namespace_id, key)
);

CREATE INDEX idx_cluster_templates_namespace_created_at ON cluster_templates(namespace_id, created_at);

CREATE TABLE cluster_template_derivatives (
  source_template_id TEXT NOT NULL REFERENCES cluster_templates(id) ON DELETE CASCADE,
  node_id TEXT NOT NULL REFERENCES cluster_nodes(id) ON DELETE CASCADE,
  derivative_template_id TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY(source_template_id, node_id)
);

CREATE TABLE cluster_environments (
  namespace_id TEXT NOT NULL REFERENCES cluster_namespaces(id) ON DELETE RESTRICT,
  id TEXT PRIMARY KEY,
  key TEXT,
  status TEXT NOT NULL,
  source_template_id TEXT NOT NULL REFERENCES cluster_templates(id) ON DELETE RESTRICT,
  node_id TEXT NOT NULL REFERENCES cluster_nodes(id) ON DELETE RESTRICT,
  derivative_template_id TEXT NOT NULL,
  derivative_environment_id TEXT NOT NULL,
  tags TEXT NOT NULL DEFAULT '[]',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  last_error TEXT NOT NULL DEFAULT '',
  UNIQUE(namespace_id, key)
);

CREATE INDEX idx_cluster_environments_namespace_created_at ON cluster_environments(namespace_id, created_at);
CREATE INDEX idx_cluster_environments_node_id ON cluster_environments(node_id);
