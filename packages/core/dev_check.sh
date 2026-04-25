#!/usr/bin/env bash
set -euo pipefail

DB_PATH="../../.bastion/sqlite.db"
TIMEOUT=60
WAITED=0

while [ ! -f "$DB_PATH" ]; do
  sleep 1
  WAITED=$((WAITED + 1))
  if [ "$WAITED" -ge "$TIMEOUT" ]; then
    echo "Timeout waiting for $DB_PATH" >&2
    exit 1
  fi
done
