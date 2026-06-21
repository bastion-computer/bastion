#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CORE_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_DIR="$(cd "$CORE_DIR/.." && pwd)"

BASTION="$CORE_DIR/tmp/bastion"
HOST_API_URL="${BASTION_HOST_API_URL:-${BASTION_API_URL:-http://localhost:3148}}"
DATA_DIR="$REPO_DIR/.bastion"
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
MINIO_CONTAINER="bastion-cluster-minio-$RUN_ID"
MINIO_BUCKET="bastion-cluster-$RUN_ID"
MINIO_ENDPOINT=""
PG_URL=""
CLUSTER_PID=""
NODE_IDS=()
NAMESPACE_IDS=()
HOST_TEMPLATE_IDS=()
HOST_ENV_IDS=()
VM_NODE_URL=""

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

run_cli() {
  "$BASTION" --api-url "$CLUSTER_URL" "$@"
}

run_host_cli() {
  "$BASTION" --api-url "$HOST_API_URL" "$@"
}

cleanup() {
  local status=$?
  set +e

  if [ -n "$CLUSTER_PID" ]; then
    kill "$CLUSTER_PID" >/dev/null 2>&1 || true
    wait "$CLUSTER_PID" >/dev/null 2>&1 || true
  fi

  if [ -n "$PG_URL" ] && [ -z "${BASTION_CLUSTER_DATABASE_URL:-}" ]; then
    docker rm -f "$PG_CONTAINER" >/dev/null 2>&1 || true
  fi

  docker rm -f "$MINIO_CONTAINER" >/dev/null 2>&1 || true

  local env_id
  for env_id in "${HOST_ENV_IDS[@]}"; do
    if [ -n "$env_id" ]; then
      run_host_cli env remove --id "$env_id" >/dev/null 2>&1 || log "cleanup: host environment $env_id was not removed"
    fi
  done

  local template_id
  for template_id in "${HOST_TEMPLATE_IDS[@]}"; do
    if [ -n "$template_id" ]; then
      run_host_cli templates remove --id "$template_id" >/dev/null 2>&1 || log "cleanup: host template $template_id was not removed"
    fi
  done

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
  require_command tar

  if [ ! -x "$BASTION" ]; then
    fail "CLI build not found at $BASTION; run mise run //core:build or mise run //core:test:e2e"
  fi

  if ! "$BASTION" start cluster --help >/dev/null 2>&1; then
    fail "bastion start cluster is not available"
  fi

  if [ -z "${BASTION_CLUSTER_DATABASE_URL:-}" ]; then
    require_command docker
  fi

  require_command docker

  if ! run_host_cli templates list >/dev/null 2>&1; then
    fail "Bastion host API is not reachable on $HOST_API_URL; start the host API and daemon before running cluster E2E"
  fi

  if ! "$BASTION" system --data-dir "$DATA_DIR" check >/dev/null 2>&1; then
    fail "Bastion system check is not ok for $DATA_DIR; run bastion system --data-dir '$DATA_DIR' add cloud-hypervisor"
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

minio_mc() {
  local command="$*"
  docker run --rm --network "container:$MINIO_CONTAINER" --entrypoint /bin/sh minio/mc -c "mc alias set local http://127.0.0.1:9000 minioadmin minioadmin >/dev/null && mc $command"
}

start_minio() {
  log "starting MinIO container"
  docker run -d --rm \
    --name "$MINIO_CONTAINER" \
    -e MINIO_ROOT_USER=minioadmin \
    -e MINIO_ROOT_PASSWORD=minioadmin \
    -p 127.0.0.1::9000 \
    minio/minio server /data --console-address :9001 >/dev/null

  local minio_port
  minio_port="$(docker inspect -f '{{(index (index .NetworkSettings.Ports "9000/tcp") 0).HostPort}}' "$MINIO_CONTAINER")"
  MINIO_ENDPOINT="http://127.0.0.1:$minio_port"

  local i=0
  until curl -fsS "$MINIO_ENDPOINT/minio/health/ready" >/dev/null 2>&1; do
    i=$((i + 1))
    if [ "$i" -gt 60 ]; then
      fail "MinIO did not become ready"
    fi
    sleep 0.5
  done

  i=0
  until minio_mc mb --ignore-existing "local/$MINIO_BUCKET" >/dev/null 2>&1; do
    i=$((i + 1))
    if [ "$i" -gt 60 ]; then
      fail "MinIO bucket $MINIO_BUCKET was not created"
    fi
    sleep 0.5
  done
}

start_cluster() {
  log "starting cluster server on $CLUSTER_ADDR"
  "$BASTION" start cluster \
    --addr "$CLUSTER_ADDR" \
    --database-url "$PG_URL" \
    --s3-bucket "$MINIO_BUCKET" \
    --s3-endpoint "$MINIO_ENDPOINT" \
    --s3-region us-east-1 \
    --s3-access-key-id minioadmin \
    --s3-secret-access-key minioadmin \
    --s3-use-path-style \
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

assert_cluster_health() {
  local output
  output="$(curl -fsS "$CLUSTER_URL/v1/health")"
  if ! jq -e '.status == "ok"' <<<"$output" >/dev/null; then
    fail "cluster health is $(jq -c . <<<"$output"), want ok"
  fi
}

assert_cluster_utilization_shape() {
  local output
  output="$(run_cli utilization)"
  if ! jq -e '
    (.vcpu.total | type == "number") and (.vcpu.used | type == "number") and (.vcpu.available | type == "number") and
    (.memory.total | type == "number") and (.memory.used | type == "number") and (.memory.available | type == "number") and
    (.volume.total | type == "number") and (.volume.used | type == "number") and (.volume.available | type == "number")
  ' <<<"$output" >/dev/null; then
    fail "cluster utilization shape is $(jq -c . <<<"$output"), want resource totals"
  fi
}

vm_node_template_config() {
  jq -nc '{
    agents: {opencode: {}},
    tunnels: {nodeapi: 4148},
    actions: {
      init: [
        {run: "set -eu\nexport DEBIAN_FRONTEND=noninteractive\napt-get update\napt-get install -y --no-install-recommends ca-certificates curl jq tar sudo bash"},
        {run: "set -eu\nrm -f /swapfile\nfallocate -l 2G /swapfile || dd if=/dev/zero of=/swapfile bs=1M count=2048\nchmod 600 /swapfile\nmkswap /swapfile\nswapon /swapfile"},
        {run: "set -eu\nmodprobe kvm_intel 2>/dev/null || modprobe kvm_amd 2>/dev/null || true\ntest -e /dev/kvm\ntest -r /dev/kvm\ntest -w /dev/kvm"}
      ]
    }
  }'
}

copy_repo_to_vm_node() {
  local env_id=$1

  log "copying repository into VM node $env_id"
  tar \
    --exclude='./.git' \
    --exclude='./.bastion' \
    --exclude='./node_modules' \
    -C "$REPO_DIR" \
    -cf - . | run_host_cli ssh --id "$env_id" -- 'rm -rf /root/bastion && mkdir -p /root/bastion && tar -xf - -C /root/bastion'
}

start_inner_bastion_node() {
  local env_id=$1

  log "starting inner Bastion API node in VM $env_id"
  run_host_cli ssh --id "$env_id" -- bash -s <<'INNER'
set -euo pipefail

cd /root/bastion

INNER_DATA_DIR=/root/.bastion-cluster-node
INNER_SOCKET=/run/bastion-cluster-node/bastiond.sock
INNER_API=http://127.0.0.1:4148

choose_inner_network_prefix() {
  local route second next
  route="$(ip -4 route get 1.1.1.1 2>/dev/null || true)"
  if [[ "$route" =~ src[[:space:]]+10\.([0-9]+)\. ]]; then
    second="${BASH_REMATCH[1]}"
    next=$((10#$second + 1))
    if [ "$next" -le 255 ]; then
      printf '10.%d\n' "$next"
      return
    fi
  fi

  printf '10.242\n'
}

rm -rf "$INNER_DATA_DIR"
mkdir -p "$INNER_DATA_DIR" /run/bastion-cluster-node

./core/tmp/bastion system --data-dir "$INNER_DATA_DIR" add cloud-hypervisor --with-utilities
./core/tmp/bastion system --data-dir "$INNER_DATA_DIR" check
sync
echo 3 > /proc/sys/vm/drop_caches || true

INNER_NETWORK_PREFIX="$(choose_inner_network_prefix)"

BASTION_DATA_DIR="$INNER_DATA_DIR" \
BASTIOND_SOCKET="$INNER_SOCKET" \
BASTION_VM_CPUS=1 \
BASTION_VM_MEMORY_BYTES=805306368 \
BASTION_VM_NETWORK_PREFIX="$INNER_NETWORK_PREFIX" \
setsid -f ./core/tmp/bastion --data-dir "$INNER_DATA_DIR" start daemon --socket "$INNER_SOCKET" --log-format text --log-level debug >/tmp/bastion-cluster-node-daemon.log 2>&1

BASTION_DATA_DIR="$INNER_DATA_DIR" \
BASTIOND_SOCKET="$INNER_SOCKET" \
setsid -f ./core/tmp/bastion --data-dir "$INNER_DATA_DIR" start api --addr 127.0.0.1:4148 --bastiond-socket "$INNER_SOCKET" --log-format text --log-level debug >/tmp/bastion-cluster-node-api.log 2>&1

for _ in $(seq 1 180); do
  if ./core/tmp/bastion --api-url "$INNER_API" templates list >/dev/null 2>&1; then
    exit 0
  fi
  sleep 1
done

cat /tmp/bastion-cluster-node-api.log >&2 || true
cat /tmp/bastion-cluster-node-daemon.log >&2 || true
exit 1
INNER
}

start_vm_node() {
  local template_key="$RUN_ID-vm-node-template"
  local template_output template_id env_output env_id

  log "creating VM-backed cluster node template"
  template_output="$(run_host_cli templates create --key "$template_key" --config "$(vm_node_template_config)")"
  template_id="$(jq -r '.id' <<<"$template_output")"
  if [ -z "$template_id" ] || [ "$template_id" = "null" ]; then
    fail "VM node template did not return an id"
  fi
  HOST_TEMPLATE_IDS+=("$template_id")

  log "creating VM-backed cluster node environment"
  env_output="$(run_host_cli env create --template-key "$template_key")"
  env_id="$(jq -r '.id' <<<"$env_output")"
  if [ -z "$env_id" ] || [ "$env_id" = "null" ]; then
    fail "VM node environment did not return an id"
  fi
  HOST_ENV_IDS+=("$env_id")

  copy_repo_to_vm_node "$env_id"
  start_inner_bastion_node "$env_id"

  VM_NODE_URL="${HOST_API_URL%/}/v1/environments/$env_id/tunnels/nodeapi"

  local i=0
  until curl -fsS "$VM_NODE_URL/v1/health" >/dev/null 2>&1; do
    i=$((i + 1))
    if [ "$i" -gt 120 ]; then
      fail "VM-backed cluster node did not become reachable at $VM_NODE_URL"
    fi
    sleep 1
  done
}

run_vm_node_case() {
  local created node_id listed got

  start_vm_node

  log "registering VM-backed cluster node"
  created="$(run_cli cluster nodes create --key "$RUN_ID-vm-node" --url "$VM_NODE_URL")"
  node_id="$(jq -r '.id' <<<"$created")"
  NODE_IDS+=("$node_id")

  assert_node "VM-backed node" "$created" "$RUN_ID-vm-node" "$VM_NODE_URL"

  listed="$(run_cli cluster nodes list --limit 100)"
  if ! jq -e --arg id "$node_id" '.entries[] | select(.id == $id)' <<<"$listed" >/dev/null; then
    fail "node list did not include VM-backed node: $(jq -c '.entries | map(.id)' <<<"$listed")"
  fi

  got="$(run_cli cluster nodes get --key "$RUN_ID-vm-node")"
  assert_node "get VM-backed node by key" "$got" "$RUN_ID-vm-node" "$VM_NODE_URL"

  assert_cluster_health
  assert_cluster_utilization_shape
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

cluster_template_config() {
  local secret_key=$1

  jq -nc --arg secret_key "$secret_key" '{
    agents: {opencode: {}},
    actions: {
      init: [
        {run: ("set -eu\nmkdir -p /opt/bastion-cluster-e2e\nprintf \"${{ secret." + $secret_key + " }}\" > /opt/bastion-cluster-e2e/secret")}
      ]
    }
  }'
}

assert_command_fails() {
  if "$@" >/dev/null 2>&1; then
    fail "command unexpectedly succeeded: $*"
  fi
}

assert_node_derivatives_removed() {
  local templates secrets

  templates="$(curl -fsS "$VM_NODE_URL/v1/templates?limit=100")"
  if jq -e '.entries | length != 0' <<<"$templates" >/dev/null; then
    fail "node derivative templates were not removed: $(jq -c . <<<"$templates")"
  fi

  secrets="$(curl -fsS "$VM_NODE_URL/v1/secrets?limit=100")"
  if jq -e '.entries | length != 0' <<<"$secrets" >/dev/null; then
    fail "node derivative secrets were not removed: $(jq -c . <<<"$secrets")"
  fi
}

assert_minio_object_exists() {
  local namespace_id=$1
  local template_id=$2

  if ! minio_mc stat "local/$MINIO_BUCKET/templates/$namespace_id/$template_id.tar.zst" >/dev/null 2>&1; then
    fail "template archive object for $template_id was not found in MinIO"
  fi
}

assert_minio_object_missing() {
  local namespace_id=$1
  local template_id=$2

  if minio_mc stat "local/$MINIO_BUCKET/templates/$namespace_id/$template_id.tar.zst" >/dev/null 2>&1; then
    fail "template archive object for $template_id was left orphaned in MinIO"
  fi
}

run_resource_case() {
  local ns_a ns_b secret_key secret_value secret_output secret_id template_key template_output template_id got listed archive imported_key imported_output imported_id imported_archive

  log "creating resource namespaces"
  ns_a="$(jq -r '.id' <<<"$(run_cli cluster namespaces create --key "$RUN_ID-resources-a")")"
  ns_b="$(jq -r '.id' <<<"$(run_cli cluster namespaces create --key "$RUN_ID-resources-b")")"
  NAMESPACE_IDS+=("$ns_a" "$ns_b")

  secret_key="$RUN_ID-secret"
  secret_value="cluster-secret-value-$RUN_ID"
  template_key="$RUN_ID-template"
  imported_key="$RUN_ID-imported"
  archive="$WORK_DIR/source-template.tar.zst"
  imported_archive="$WORK_DIR/imported-template.tar.zst"

  log "creating source secret on namespace $ns_a"
  secret_output="$(run_cli --namespace-id "$ns_a" secrets create --key "$secret_key" --value "$secret_value")"
  secret_id="$(jq -r '.id' <<<"$secret_output")"
  if [[ "$secret_id" != sec_* ]]; then
    fail "cluster secret id is $secret_id, want sec_ prefix"
  fi

  got="$(run_cli --namespace-key "$RUN_ID-resources-a" secrets get --key "$secret_key")"
  if ! jq -e --arg id "$secret_id" --arg key "$secret_key" --arg value "$secret_value" '.id == $id and .key == $key and .value == $value' <<<"$got" >/dev/null; then
    fail "cluster secret get returned $(jq -c . <<<"$got"), want source secret"
  fi

  assert_command_fails run_cli --namespace-id "$ns_b" secrets get --key "$secret_key"
  assert_command_fails run_cli secrets get --key "$secret_key"

  log "creating source template on namespace $ns_a"
  template_output="$(run_cli --namespace-id "$ns_a" templates create --key "$template_key" --config "$(cluster_template_config "$secret_key")")"
  template_id="$(jq -r '.id' <<<"$template_output")"
  if [[ "$template_id" != tpl_* ]]; then
    fail "cluster template id is $template_id, want tpl_ prefix"
  fi

  assert_node_derivatives_removed
  assert_minio_object_exists "$ns_a" "$template_id"

  got="$(run_cli --namespace-key "$RUN_ID-resources-a" templates get --key "$template_key")"
  if ! jq -e --arg id "$template_id" --arg key "$template_key" '.id == $id and .key == $key' <<<"$got" >/dev/null; then
    fail "cluster template get returned $(jq -c . <<<"$got"), want source template"
  fi
  if [[ "$got" != *"\${{ secret.$secret_key }}"* ]]; then
    fail "cluster template get did not preserve source secret reference: $got"
  fi
  if [[ "$got" == *"$secret_value"* ]]; then
    fail "cluster template get leaked secret value: $got"
  fi

  listed="$(run_cli --namespace-id "$ns_a" templates list --limit 100)"
  if ! jq -e --arg id "$template_id" '.entries[] | select(.id == $id)' <<<"$listed" >/dev/null; then
    fail "cluster template list did not include $template_id"
  fi

  assert_command_fails run_cli --namespace-id "$ns_b" templates get --key "$template_key"
  assert_command_fails run_cli templates get --key "$template_key"

  log "exporting source template from namespace $ns_a"
  run_cli --namespace-id "$ns_a" templates export --key "$template_key" >"$archive"
  if [ ! -s "$archive" ]; then
    fail "cluster template export did not write an archive"
  fi

  assert_command_fails run_cli --namespace-id "$ns_b" templates export --key "$template_key"
  assert_command_fails run_cli templates export --key "$template_key"

  log "importing exported template into namespace $ns_a"
  imported_output="$(run_cli --namespace-id "$ns_a" templates import --key "$imported_key" --file "$archive")"
  imported_id="$(jq -r '.id' <<<"$imported_output")"
  if [[ "$imported_id" != tpl_* ]] || [ "$imported_id" = "$template_id" ]; then
    fail "imported template id is $imported_id, want new tpl_ id"
  fi

  got="$(run_cli --namespace-id "$ns_a" templates get --key "$imported_key")"
  if [[ "$got" != *"\${{ secret.$secret_key }}"* ]]; then
    fail "imported template get did not preserve source secret reference: $got"
  fi

  run_cli --namespace-id "$ns_a" templates export --id "$imported_id" >"$imported_archive"
  if [ ! -s "$imported_archive" ]; then
    fail "imported template export did not write an archive"
  fi

  assert_command_fails run_cli templates import --key "$RUN_ID-import-no-namespace" --file "$archive"

  log "removing imported template and verifying archive cleanup"
  run_cli --namespace-id "$ns_a" templates remove --id "$imported_id" >/dev/null
  assert_minio_object_missing "$ns_a" "$imported_id"

  log "removing source template and verifying archive cleanup"
  run_cli --namespace-id "$ns_a" templates remove --id "$template_id" >/dev/null
  assert_minio_object_missing "$ns_a" "$template_id"

  run_cli --namespace-id "$ns_a" secrets remove --id "$secret_id" >/dev/null
  run_cli cluster namespaces remove --id "$ns_a" >/dev/null
  run_cli cluster namespaces remove --id "$ns_b" >/dev/null
}

main() {
  precheck
  trap cleanup EXIT

  start_postgres
  start_minio
  start_cluster
  run_namespace_case
  run_vm_node_case
  run_resource_case
  log "cluster e2e run passed"
}

main "$@"
