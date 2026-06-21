CREATE TABLE cluster_nodes (
  id TEXT PRIMARY KEY,
  key TEXT,
  url TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX idx_cluster_nodes_created_at ON cluster_nodes(created_at);
CREATE UNIQUE INDEX idx_cluster_nodes_key ON cluster_nodes(key) WHERE key IS NOT NULL;

CREATE TABLE cluster_namespaces (
  id TEXT PRIMARY KEY,
  key TEXT,
  created_at TEXT NOT NULL
);

CREATE INDEX idx_cluster_namespaces_created_at ON cluster_namespaces(created_at);
CREATE UNIQUE INDEX idx_cluster_namespaces_key ON cluster_namespaces(key) WHERE key IS NOT NULL;
