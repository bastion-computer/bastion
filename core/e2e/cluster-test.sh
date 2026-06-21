#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CORE_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

BASTION="$CORE_DIR/tmp/bastion"
RUN_ID="e2e-cluster-$(date +%Y%m%d%H%M%S)-$$"
WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/bastion-cluster-e2e.XXXXXX")"
CLUSTER_PORT="$(python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
)"
CLUSTER_ADDR="127.0.0.1:$CLUSTER_PORT"
CLUSTER_URL="http://$CLUSTER_ADDR"
PG_CONTAINER="bastion-cluster-$RUN_ID"
PG_URL=""
CLUSTER_PID=""
NODE_PIDS=()
NODE_IDS=()
NAMESPACE_IDS=()

log() {
  printf '[cluster-test] %s\n' "$*"
}

fail() {
  log "FAIL: $*" >&2
  exit 1
}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    fail "$1 is required"
  fi
}

free_port() {
  python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
}

run_cli() {
  "$BASTION" --api-url "$CLUSTER_URL" "$@"
}

cleanup() {
  local status=$?
  set +e

  if [ -n "$CLUSTER_PID" ]; then
    kill "$CLUSTER_PID" >/dev/null 2>&1 || true
    wait "$CLUSTER_PID" >/dev/null 2>&1 || true
  fi

  local node_pid
  for node_pid in "${NODE_PIDS[@]}"; do
    if [ -n "$node_pid" ]; then
      kill "$node_pid" >/dev/null 2>&1 || true
      wait "$node_pid" >/dev/null 2>&1 || true
    fi
  done

  if [ -n "$PG_URL" ] && [ -z "${BASTION_CLUSTER_DATABASE_URL:-}" ]; then
    docker rm -f "$PG_CONTAINER" >/dev/null 2>&1 || true
  fi

  if [ "$status" -ne 0 ]; then
    log "cluster log: $WORK_DIR/cluster.log"
  fi

  rm -rf "$WORK_DIR"
  exit "$status"
}

precheck() {
  require_command curl
  require_command jq
  require_command python3

  if [ ! -x "$BASTION" ]; then
    fail "CLI build not found at $BASTION; run mise run //core:build or mise run //core:test:e2e"
  fi

  if ! "$BASTION" start cluster --help >/dev/null 2>&1; then
    fail "bastion start cluster is not available"
  fi

  if [ -z "${BASTION_CLUSTER_DATABASE_URL:-}" ]; then
    require_command docker
  fi
}

start_postgres() {
  if [ -n "${BASTION_CLUSTER_DATABASE_URL:-}" ]; then
    PG_URL="$BASTION_CLUSTER_DATABASE_URL"
    return
  fi

  log "starting Postgres 18 container"
  docker run -d --rm \
    --name "$PG_CONTAINER" \
    -e POSTGRES_USER=bastion \
    -e POSTGRES_PASSWORD=bastion \
    -e POSTGRES_DB=bastion_cluster \
    -p 127.0.0.1::5432 \
    postgres:18 >/dev/null

  local pg_port
  pg_port="$(docker inspect -f '{{(index (index .NetworkSettings.Ports "5432/tcp") 0).HostPort}}' "$PG_CONTAINER")"
  PG_URL="postgres://bastion:bastion@127.0.0.1:$pg_port/bastion_cluster?sslmode=disable"

  local i=0
  until docker exec "$PG_CONTAINER" pg_isready -U bastion -d bastion_cluster >/dev/null 2>&1; do
    i=$((i + 1))
    if [ "$i" -gt 60 ]; then
      fail "Postgres did not become ready"
    fi
    sleep 0.5
  done

  i=0
  until [ "$(docker exec "$PG_CONTAINER" psql -U bastion -d bastion_cluster -tAc 'SELECT 1' 2>/dev/null | tr -d '[:space:]')" = "1" ]; do
    i=$((i + 1))
    if [ "$i" -gt 60 ]; then
      fail "Postgres did not accept SQL connections"
    fi
    sleep 0.5
  done
}

start_cluster() {
  log "starting cluster server on $CLUSTER_ADDR"
  "$BASTION" start cluster \
    --addr "$CLUSTER_ADDR" \
    --database-url "$PG_URL" \
    --log-format text \
    --log-level debug >"$WORK_DIR/cluster.log" 2>&1 &
  CLUSTER_PID=$!

  local i=0
  until curl -fsS "$CLUSTER_URL/v1/health" >/dev/null 2>&1; do
    i=$((i + 1))
    if [ "$i" -gt 60 ]; then
      fail "cluster server did not become ready: $(<"$WORK_DIR/cluster.log")"
    fi
    sleep 0.2
  done
}

write_node_server() {
  cat >"$WORK_DIR/node_server.py" <<'PY'
import json
import os
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

utilization = json.loads(os.environ["UTILIZATION_JSON"])

class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/v1/health":
            self.respond({"status": "ok"})
            return
        if self.path == "/v1/utilization":
            self.respond(utilization)
            return
        self.send_response(404)
        self.end_headers()

    def respond(self, body):
        encoded = json.dumps(body).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(encoded)))
        self.end_headers()
        self.wfile.write(encoded)

    def log_message(self, *_):
        return

port = int(os.environ["PORT"])
ThreadingHTTPServer(("127.0.0.1", port), Handler).serve_forever()
PY
}

start_node() {
  local port=$1
  local utilization_json=$2

  PORT="$port" UTILIZATION_JSON="$utilization_json" python3 "$WORK_DIR/node_server.py" &
  NODE_PIDS+=("$!")

  local i=0
  until curl -fsS "http://127.0.0.1:$port/v1/health" >/dev/null 2>&1; do
    i=$((i + 1))
    if [ "$i" -gt 50 ]; then
      fail "fake node on port $port did not become ready"
    fi
    sleep 0.1
  done
}

assert_node() {
  local label=$1
  local json=$2
  local key=$3
  local url=$4

  if ! jq -e --arg key "$key" --arg url "$url" '.id | startswith("node_")' <<<"$json" >/dev/null; then
    fail "$label id is $(jq -r '.id // null' <<<"$json"), want node_ prefix"
  fi

  if [ -n "$key" ]; then
    if ! jq -e --arg key "$key" '.key == $key' <<<"$json" >/dev/null; then
      fail "$label key is $(jq -c '.key // null' <<<"$json"), want $key"
    fi
  else
    if jq -e 'has("key")' <<<"$json" >/dev/null; then
      fail "$label unexpectedly has key: $(jq -c '.key' <<<"$json")"
    fi
  fi

  if ! jq -e --arg url "$url" '.url == $url' <<<"$json" >/dev/null; then
    fail "$label url is $(jq -c '.url // null' <<<"$json"), want $url"
  fi
}

assert_namespace() {
  local label=$1
  local json=$2
  local key=$3

  if ! jq -e '.id | startswith("ns_")' <<<"$json" >/dev/null; then
    fail "$label id is $(jq -r '.id // null' <<<"$json"), want ns_ prefix"
  fi

  if [ -n "$key" ]; then
    if ! jq -e --arg key "$key" '.key == $key' <<<"$json" >/dev/null; then
      fail "$label key is $(jq -c '.key // null' <<<"$json"), want $key"
    fi
  else
    if jq -e 'has("key")' <<<"$json" >/dev/null; then
      fail "$label unexpectedly has key: $(jq -c '.key' <<<"$json")"
    fi
  fi
}

run_node_case() {
  local node_one_port node_two_port node_one_url node_two_url created_one created_two node_one_id node_two_id listed got

  node_one_port="$(free_port)"
  node_two_port="$(free_port)"
  node_one_url="http://127.0.0.1:$node_one_port"
  node_two_url="http://127.0.0.1:$node_two_port"

  write_node_server
  start_node "$node_one_port" '{"vcpu":{"total":4,"used":1,"available":3},"memory":{"total":1000,"used":250,"available":750},"volume":{"total":10000,"used":1000,"available":9000}}'
  start_node "$node_two_port" '{"vcpu":{"total":8,"used":2,"available":6},"memory":{"total":2000,"used":500,"available":1500},"volume":{"total":20000,"used":2000,"available":18000}}'

  log "creating cluster nodes"
  created_one="$(run_cli cluster nodes create --key "$RUN_ID-node-a" --url "$node_one_url")"
  created_two="$(run_cli cluster nodes create --url "$node_two_url")"
  node_one_id="$(jq -r '.id' <<<"$created_one")"
  node_two_id="$(jq -r '.id' <<<"$created_two")"
  NODE_IDS+=("$node_one_id" "$node_two_id")

  assert_node "keyed node" "$created_one" "$RUN_ID-node-a" "$node_one_url"
  assert_node "unkeyed node" "$created_two" "" "$node_two_url"

  listed="$(run_cli cluster nodes list --limit 100)"
  if ! jq -e --arg one "$node_one_id" --arg two "$node_two_id" '([.entries[].id] | index($one) and index($two))' <<<"$listed" >/dev/null; then
    fail "node list did not include created nodes: $(jq -c '.entries | map(.id)' <<<"$listed")"
  fi

  got="$(run_cli cluster nodes get --key "$RUN_ID-node-a")"
  assert_node "get node by key" "$got" "$RUN_ID-node-a" "$node_one_url"

  got="$(run_cli cluster nodes get --id "$node_two_id")"
  assert_node "get node by id" "$got" "" "$node_two_url"

  assert_cluster_health
  assert_cluster_utilization

  run_cli cluster nodes remove --key "$RUN_ID-node-a" >/dev/null
  run_cli cluster nodes remove --id "$node_two_id" >/dev/null
  NODE_IDS=()

  listed="$(run_cli cluster nodes list --limit 100)"
  if jq -e --arg one "$node_one_id" --arg two "$node_two_id" '.entries[] | select(.id == $one or .id == $two)' <<<"$listed" >/dev/null; then
    fail "removed nodes still listed: $(jq -c '.entries | map(.id)' <<<"$listed")"
  fi
}

assert_cluster_health() {
  local output
  output="$(curl -fsS "$CLUSTER_URL/v1/health")"
  if ! jq -e '.status == "ok"' <<<"$output" >/dev/null; then
    fail "cluster health is $(jq -c . <<<"$output"), want ok"
  fi
}

assert_cluster_utilization() {
  local output
  output="$(run_cli utilization)"
  if ! jq -e '
    .vcpu == {"total":12,"used":3,"available":9} and
    .memory == {"total":3000,"used":750,"available":2250} and
    .volume == {"total":30000,"used":3000,"available":27000}
  ' <<<"$output" >/dev/null; then
    fail "cluster utilization is $(jq -c . <<<"$output"), want aggregate node values"
  fi
}

run_namespace_case() {
  local created_one created_two ns_one_id ns_two_id listed got

  log "creating cluster namespaces"
  created_one="$(run_cli cluster namespaces create --key "$RUN_ID-ns-a")"
  created_two="$(run_cli cluster namespaces create)"
  ns_one_id="$(jq -r '.id' <<<"$created_one")"
  ns_two_id="$(jq -r '.id' <<<"$created_two")"
  NAMESPACE_IDS+=("$ns_one_id" "$ns_two_id")

  assert_namespace "keyed namespace" "$created_one" "$RUN_ID-ns-a"
  assert_namespace "unkeyed namespace" "$created_two" ""

  listed="$(run_cli cluster namespaces list --limit 100)"
  if ! jq -e --arg one "$ns_one_id" --arg two "$ns_two_id" '([.entries[].id] | index($one) and index($two))' <<<"$listed" >/dev/null; then
    fail "namespace list did not include created namespaces: $(jq -c '.entries | map(.id)' <<<"$listed")"
  fi

  got="$(run_cli cluster namespaces get --key "$RUN_ID-ns-a")"
  assert_namespace "get namespace by key" "$got" "$RUN_ID-ns-a"

  got="$(run_cli cluster namespaces get --id "$ns_two_id")"
  assert_namespace "get namespace by id" "$got" ""

  run_cli cluster namespaces remove --key "$RUN_ID-ns-a" >/dev/null
  run_cli cluster namespaces remove --id "$ns_two_id" >/dev/null
  NAMESPACE_IDS=()

  listed="$(run_cli cluster namespaces list --limit 100)"
  if jq -e --arg one "$ns_one_id" --arg two "$ns_two_id" '.entries[] | select(.id == $one or .id == $two)' <<<"$listed" >/dev/null; then
    fail "removed namespaces still listed: $(jq -c '.entries | map(.id)' <<<"$listed")"
  fi
}

main() {
  precheck
  trap cleanup EXIT

  start_postgres
  start_cluster
  run_node_case
  run_namespace_case
  log "cluster e2e run passed"
}

main "$@"
