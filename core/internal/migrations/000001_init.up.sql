CREATE TABLE templates (
  id TEXT PRIMARY KEY,
  key TEXT,
  config TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE "environments" (
  id TEXT PRIMARY KEY,
  key TEXT,
  status TEXT NOT NULL,
  template_id TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL DEFAULT '',
  last_error TEXT NOT NULL DEFAULT '',
  FOREIGN KEY (template_id) REFERENCES templates(id) ON DELETE RESTRICT
);

CREATE TABLE environment_vms (
  environment_id TEXT PRIMARY KEY,
  vm_id TEXT NOT NULL,
  state TEXT NOT NULL,
  pid INTEGER NOT NULL DEFAULT 0,
  env_dir TEXT NOT NULL,
  runtime_dir TEXT NOT NULL,
  socket_path TEXT NOT NULL,
  kernel_path TEXT NOT NULL,
  rootfs_path TEXT NOT NULL,
  tap_name TEXT NOT NULL,
  host_ip TEXT NOT NULL,
  guest_ip TEXT NOT NULL,
  guest_cidr TEXT NOT NULL,
  guest_mac TEXT NOT NULL,
  ssh_user TEXT NOT NULL,
  ssh_port INTEGER NOT NULL,
  ssh_key_path TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  last_error TEXT NOT NULL DEFAULT '',
  FOREIGN KEY (environment_id) REFERENCES environments(id) ON DELETE CASCADE
);

CREATE TABLE environment_tags (
  environment_id TEXT NOT NULL,
  tag TEXT NOT NULL,
  position INTEGER NOT NULL,
  PRIMARY KEY (environment_id, position),
  FOREIGN KEY (environment_id) REFERENCES environments(id) ON DELETE CASCADE
);

CREATE INDEX idx_templates_created_at ON templates(created_at);
CREATE UNIQUE INDEX idx_templates_key ON templates(key) WHERE key IS NOT NULL;
CREATE INDEX idx_environments_created_at ON environments(created_at);
CREATE UNIQUE INDEX idx_environments_key ON environments(key) WHERE key IS NOT NULL;
CREATE INDEX idx_environment_vms_state ON environment_vms(state);
CREATE INDEX idx_environment_tags_tag ON environment_tags(tag);
