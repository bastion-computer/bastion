#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CORE_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_DIR="$(cd "$CORE_DIR/.." && pwd)"

BASTION="$CORE_DIR/tmp/bastion"
API_URL="${BASTION_API_URL:-http://localhost:3148}"
DATA_DIR="$REPO_DIR/.bastion"
RUN_ID="e2e-queue-$(date +%Y%m%d%H%M%S)-$$"

QUEUE_KEY="$RUN_ID-queue"
TEMPLATE_KEY="$RUN_ID-template"
ENV_KEY="$RUN_ID-env"
FUNCTION_NAME="queue_e2e_handler"
FUNCTION_DIR="$DATA_DIR/functions/$FUNCTION_NAME"

QUEUE_ID=""
TEMPLATE_ID=""
ENV_ID=""

log() {
  printf '[queue-test] %s\n' "$*"
}

fail() {
  log "FAIL: $*" >&2
  exit 1
}

run_cli() {
  "$BASTION" --api-url "$API_URL" "$@"
}

cleanup() {
  local status=$?
  set +e

  if [ "$status" -ne 0 ] && [ "${BASTION_E2E_KEEP_FAILED:-}" = "1" ]; then
    log "preserving failed run resources: env=$ENV_ID template=$TEMPLATE_ID queue=$QUEUE_ID function=$FUNCTION_DIR"
    exit "$status"
  fi

  if [ -n "$ENV_ID" ]; then
    run_cli env remove --id "$ENV_ID" >/dev/null 2>&1 || log "cleanup: environment $ENV_ID was not removed"
  fi

  if [ -n "$TEMPLATE_ID" ]; then
    run_cli templates remove --id "$TEMPLATE_ID" >/dev/null 2>&1 || log "cleanup: template $TEMPLATE_ID was not removed"
  fi

  if [ -n "$QUEUE_ID" ]; then
    run_cli queues remove --id "$QUEUE_ID" >/dev/null 2>&1 || log "cleanup: queue $QUEUE_ID was not removed"
  fi

  rm -rf -- "$FUNCTION_DIR"

  exit "$status"
}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    fail "$1 is required"
  fi
}

precheck() {
  require_command jq

  if [ ! -x "$BASTION" ]; then
    fail "CLI build not found at $BASTION; run mise run //core:build or mise run //core:test:e2e"
  fi

  if ! run_cli queues list >/dev/null 2>&1; then
    fail "Bastion API is not reachable on $API_URL; run mise dev:up"
  fi

  if ! "$BASTION" system --data-dir "$DATA_DIR" check >/dev/null 2>&1; then
    fail "Bastion system check is not ok for $DATA_DIR; run bastion system --data-dir '$DATA_DIR' add cloud-hypervisor"
  fi
}

json_get() {
  jq -r "$1"
}

write_function_package() {
  log "writing mock function package $FUNCTION_NAME"
  mkdir -p "$FUNCTION_DIR"

  cat >"$FUNCTION_DIR/manifest.json" <<'JSON'
{
  "inputs": {
    "marker": {
      "type": "string",
      "description": "E2E marker returned by the handler.",
      "required": true
    }
  },
  "handler": "index.ts"
}
JSON

  cat >"$FUNCTION_DIR/index.ts" <<'TS'
type Args = {
  inputs: { marker: string };
  data: { message?: string; fail?: boolean };
};

export default async function handler({ inputs, data }: Args) {
  if (data.fail) {
    throw new Error(`intentional queue e2e failure: ${data.message ?? "missing"}`);
  }

  await Bun.write(
    "/opt/bastion-queue-e2e/result.json",
    JSON.stringify({ marker: inputs.marker, message: data.message }) + "\n",
  );

  return { ok: true, marker: inputs.marker, message: data.message };
}
TS
}

queue_template_config() {
  jq -nc --arg function_name "$FUNCTION_NAME" --arg queue_key "$QUEUE_KEY" --arg marker "$RUN_ID" '{
    functions: {
      ($function_name): {
        trigger: {type: "queue", key: $queue_key},
        with: {marker: $marker}
      }
    },
    actions: {
      init: [
        {run: "set -eu\nmkdir -p /opt/bastion-queue-e2e"}
      ]
    }
  }'
}

create_queue() {
  local output

  log "creating queue $QUEUE_KEY"
  output="$(run_cli queues create --key "$QUEUE_KEY")"
  QUEUE_ID="$(json_get '.id' <<<"$output")"

  if [ -z "$QUEUE_ID" ] || [ "$QUEUE_ID" = "null" ]; then
    fail "queue did not return an id"
  fi
}

create_template() {
  local output

  log "creating function template $TEMPLATE_KEY"
  output="$(run_cli templates create --key "$TEMPLATE_KEY" --config "$(queue_template_config)")"
  TEMPLATE_ID="$(json_get '.id' <<<"$output")"

  if [ -z "$TEMPLATE_ID" ] || [ "$TEMPLATE_ID" = "null" ]; then
    fail "template did not return an id"
  fi
}

create_environment() {
  local output

  log "creating environment $ENV_KEY"
  output="$(run_cli env create --template-key "$TEMPLATE_KEY" --key "$ENV_KEY")"
  ENV_ID="$(json_get '.id' <<<"$output")"

  if [ -z "$ENV_ID" ] || [ "$ENV_ID" = "null" ]; then
    fail "environment did not return an id"
  fi
}

publish_task() {
  local data=$1
  local retry=${2:-}
  local output
  local args=(queues publish --key "$QUEUE_KEY" --data "$data")

  if [ -n "$retry" ]; then
    args+=(--retry "$retry")
  fi

  output="$(run_cli "${args[@]}")"
  json_get '.id' <<<"$output"
}

wait_task_status() {
  local task_id=$1
  local want=$2
  local output
  local status
  local deadline=$((SECONDS + 180))

  while [ "$SECONDS" -lt "$deadline" ]; do
    output="$(run_cli queues tasks get --key "$QUEUE_KEY" --task-id "$task_id")"
    status="$(json_get '.status' <<<"$output")"

    if [ "$status" = "$want" ]; then
      printf '%s\n' "$output"
      return 0
    fi

    sleep 2
  done

  fail "task $task_id status did not become $want; last status=$status output=$output"
}

assert_success_task() {
  local task_id=$1
  local output

  output="$(wait_task_status "$task_id" complete)"
  if ! jq -e --arg run_id "$RUN_ID" '.workerData.ok == true and .workerData.marker == $run_id and .workerData.message == "hello queue"' <<<"$output" >/dev/null; then
    fail "completed task workerData is unexpected: $(jq -c '.workerData' <<<"$output")"
  fi

  run_cli ssh --id "$ENV_ID" -- test -s /opt/bastion-queue-e2e/result.json
  run_cli ssh --id "$ENV_ID" -- grep -q "$RUN_ID" /opt/bastion-queue-e2e/result.json
}

assert_dead_task() {
  local task_id=$1
  local output

  output="$(wait_task_status "$task_id" dead)"
  if ! jq -e '.lastError | contains("intentional queue e2e failure")' <<<"$output" >/dev/null; then
    fail "dead task lastError is unexpected: $(jq -c '.lastError' <<<"$output")"
  fi
}

main() {
  local success_task_id
  local fail_task_id

  precheck
  trap cleanup EXIT

  log "starting queue e2e run $RUN_ID"
  write_function_package
  create_queue
  create_template
  create_environment

  success_task_id="$(publish_task '{"message":"hello queue"}')"
  assert_success_task "$success_task_id"

  fail_task_id="$(publish_task '{"message":"bad queue","fail":true}' '{"max_attempts":1,"delay_ms":1}')"
  assert_dead_task "$fail_task_id"

  log "queue e2e run passed"
}

main "$@"
