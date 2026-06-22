CREATE TABLE cluster_template_derivatives (
  id TEXT PRIMARY KEY,
  template_id TEXT NOT NULL REFERENCES cluster_templates(id) ON DELETE CASCADE,
  node_id TEXT NOT NULL REFERENCES cluster_nodes(id) ON DELETE CASCADE,
  derivative_template_id TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE (template_id, node_id),
  UNIQUE (node_id, derivative_template_id)
);

CREATE INDEX idx_cluster_template_derivatives_template_id ON cluster_template_derivatives(template_id);
CREATE INDEX idx_cluster_template_derivatives_node_id ON cluster_template_derivatives(node_id);

CREATE TABLE cluster_template_derivative_node_secrets (
  derivative_id TEXT NOT NULL REFERENCES cluster_template_derivatives(id) ON DELETE CASCADE,
  source_secret_id TEXT NOT NULL REFERENCES cluster_secrets(id) ON DELETE CASCADE,
  derivative_secret_id TEXT NOT NULL,
  position INTEGER NOT NULL,
  PRIMARY KEY (derivative_id, position)
);

CREATE INDEX idx_cluster_template_derivative_node_secrets_derivative_id ON cluster_template_derivative_node_secrets(derivative_id);

CREATE TABLE cluster_environments (
  id TEXT PRIMARY KEY,
  namespace_id TEXT NOT NULL REFERENCES cluster_namespaces(id) ON DELETE CASCADE,
  key TEXT,
  status TEXT NOT NULL,
  template_id TEXT NOT NULL REFERENCES cluster_templates(id) ON DELETE RESTRICT,
  node_id TEXT NOT NULL REFERENCES cluster_nodes(id) ON DELETE RESTRICT,
  derivative_id TEXT NOT NULL REFERENCES cluster_template_derivatives(id) ON DELETE RESTRICT,
  derivative_environment_id TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  last_error TEXT NOT NULL DEFAULT '',
  UNIQUE (node_id, derivative_environment_id)
);

CREATE INDEX idx_cluster_environments_namespace_created_at ON cluster_environments(namespace_id, created_at);
CREATE INDEX idx_cluster_environments_template_id ON cluster_environments(template_id);
CREATE INDEX idx_cluster_environments_derivative_id ON cluster_environments(derivative_id);
CREATE UNIQUE INDEX idx_cluster_environments_namespace_key ON cluster_environments(namespace_id, key) WHERE key IS NOT NULL;

CREATE TABLE cluster_environment_tags (
  environment_id TEXT NOT NULL REFERENCES cluster_environments(id) ON DELETE CASCADE,
  tag TEXT NOT NULL,
  position INTEGER NOT NULL,
  PRIMARY KEY (environment_id, position)
);

CREATE INDEX idx_cluster_environment_tags_tag ON cluster_environment_tags(tag);
