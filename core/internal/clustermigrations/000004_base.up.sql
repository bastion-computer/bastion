CREATE TABLE cluster_base (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  content_address TEXT NOT NULL,
  archive_key TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE UNIQUE INDEX idx_cluster_base_archive_key ON cluster_base(archive_key);
