CREATE TABLE environment_tags (
  environment_id TEXT NOT NULL,
  tag TEXT NOT NULL,
  position INTEGER NOT NULL,
  PRIMARY KEY (environment_id, position),
  FOREIGN KEY (environment_id) REFERENCES environments(id) ON DELETE CASCADE
);

CREATE INDEX idx_environment_tags_tag ON environment_tags(tag);
