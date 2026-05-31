CREATE TABLE queues (
  id TEXT PRIMARY KEY,
  key TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE queue_tasks (
  id TEXT PRIMARY KEY,
  queue_id TEXT NOT NULL,
  status TEXT NOT NULL,
  data TEXT NOT NULL,
  retry_max_attempts INTEGER NOT NULL,
  retry_delay_ms INTEGER NOT NULL,
  retry_max_delay_ms INTEGER NOT NULL DEFAULT 0,
  retry_backoff_multiplier REAL NOT NULL DEFAULT 0,
  retry_jitter INTEGER NOT NULL DEFAULT 0,
  attempts INTEGER NOT NULL DEFAULT 0,
  available_at TEXT NOT NULL,
  locked_by TEXT NOT NULL DEFAULT '',
  locked_until TEXT NOT NULL DEFAULT '',
  worker_data TEXT,
  last_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  completed_at TEXT NOT NULL DEFAULT '',
  FOREIGN KEY (queue_id) REFERENCES queues(id) ON DELETE CASCADE
);

CREATE INDEX idx_queues_created_at ON queues(created_at);
CREATE UNIQUE INDEX idx_queues_key ON queues(key) WHERE key IS NOT NULL;
CREATE INDEX idx_queue_tasks_queue_status_available ON queue_tasks(queue_id, status, available_at);
CREATE INDEX idx_queue_tasks_created_at ON queue_tasks(created_at);
