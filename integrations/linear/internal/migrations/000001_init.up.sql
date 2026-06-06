CREATE TABLE IF NOT EXISTS linear_webhook_events (
  webhook_id TEXT PRIMARY KEY,
  received_at TEXT NOT NULL,
  payload TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS linear_sessions (
  agent_session_id TEXT PRIMARY KEY,
  issue_id TEXT NOT NULL DEFAULT '',
  issue_identifier TEXT NOT NULL DEFAULT '',
  issue_title TEXT NOT NULL DEFAULT '',
  team_id TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  environment_id TEXT NOT NULL DEFAULT '',
  opencode_session_id TEXT NOT NULL DEFAULT '',
  opencode_port INTEGER NOT NULL DEFAULT 0,
  opencode_pid INTEGER NOT NULL DEFAULT 0,
  stop_requested INTEGER NOT NULL DEFAULT 0,
  prompt_context TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  last_error TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS linear_jobs (
  id TEXT PRIMARY KEY,
  agent_session_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  body TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  attempts INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  last_error TEXT NOT NULL DEFAULT '',
  FOREIGN KEY (agent_session_id) REFERENCES linear_sessions(agent_session_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS linear_environment_assignments (
  environment_id TEXT PRIMARY KEY,
  agent_session_id TEXT NOT NULL UNIQUE,
  status TEXT NOT NULL,
  assigned_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (agent_session_id) REFERENCES linear_sessions(agent_session_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_linear_jobs_status_created_at ON linear_jobs(status, created_at);
CREATE INDEX IF NOT EXISTS idx_linear_sessions_environment_id ON linear_sessions(environment_id);
