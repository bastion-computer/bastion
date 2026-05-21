ALTER TABLE environments ADD COLUMN updated_at TEXT NOT NULL DEFAULT '';
ALTER TABLE environments ADD COLUMN last_error TEXT NOT NULL DEFAULT '';

UPDATE environments SET updated_at = created_at WHERE updated_at = '';

CREATE TABLE environment_vms (
  environment_id TEXT PRIMARY KEY,
  vm_id TEXT NOT NULL,
  state TEXT NOT NULL,
  pid INTEGER NOT NULL DEFAULT 0,
  env_dir TEXT NOT NULL,
  jailer_dir TEXT NOT NULL,
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

CREATE INDEX idx_environment_vms_state ON environment_vms(state);
