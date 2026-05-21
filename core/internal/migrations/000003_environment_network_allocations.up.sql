CREATE TABLE environment_network_allocations (
  environment_id TEXT PRIMARY KEY,
  network_index INTEGER NOT NULL UNIQUE,
  created_at TEXT NOT NULL,
  CHECK (network_index >= 0 AND network_index < 16000),
  FOREIGN KEY (environment_id) REFERENCES environments(id) ON DELETE CASCADE
);

WITH parsed AS (
  SELECT environment_id, substr(guest_cidr, 8) AS rest
  FROM environment_vms
  WHERE guest_cidr LIKE '10.241.%/30'
), parts AS (
  SELECT
    environment_id,
    CAST(substr(rest, 1, instr(rest, '.') - 1) AS INTEGER) AS third_octet,
    substr(rest, instr(rest, '.') + 1) AS last_octet_with_mask
  FROM parsed
  WHERE instr(rest, '.') > 0
), octets AS (
  SELECT
    environment_id,
    third_octet,
    CAST(substr(last_octet_with_mask, 1, instr(last_octet_with_mask, '/') - 1) AS INTEGER) AS guest_last_octet
  FROM parts
  WHERE instr(last_octet_with_mask, '/') > 0
), indices AS (
  SELECT
    environment_id,
    third_octet * 64 + CAST((guest_last_octet - 2) / 4 AS INTEGER) AS network_index
  FROM octets
  WHERE third_octet BETWEEN 0 AND 249
    AND guest_last_octet BETWEEN 2 AND 254
    AND (guest_last_octet - 2) % 4 = 0
)
INSERT OR IGNORE INTO environment_network_allocations (environment_id, network_index, created_at)
SELECT environment_id, network_index, strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
FROM indices
WHERE network_index >= 0 AND network_index < 16000;
