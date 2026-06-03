#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CORE_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_DIR="$(cd "$CORE_DIR/.." && pwd)"

BASTION="$CORE_DIR/tmp/bastion"
API_URL="${BASTION_API_URL:-http://localhost:3148}"
DATA_DIR="$REPO_DIR/.bastion"
RUN_ID="e2e-mux-$(date +%Y%m%d%H%M%S)-$$"
MUX_SESSION="bastion-mux-$RUN_ID"
CONTROL_SESSION="$MUX_SESSION-control"

TEMPLATE_KEY=""
ENV_ID=""

log() {
  printf '[mux-test] %s\n' "$*"
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

  tmux kill-session -t "$CONTROL_SESSION" >/dev/null 2>&1 || true
  tmux kill-session -t "$MUX_SESSION" >/dev/null 2>&1 || true

  if [ -n "$ENV_ID" ]; then
    run_cli env remove --id "$ENV_ID" >/dev/null 2>&1 || log "cleanup: environment $ENV_ID was not removed"
  fi

  if [ -n "$TEMPLATE_KEY" ]; then
    run_cli templates remove --key "$TEMPLATE_KEY" >/dev/null 2>&1 || log "cleanup: template $TEMPLATE_KEY was not removed"
  fi

  exit "$status"
}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    fail "$1 is required"
  fi
}

precheck() {
  require_command jq
  require_command tmux

  if [ ! -x "$BASTION" ]; then
    fail "CLI build not found at $BASTION; run mise run //core:build or mise run //core:test:e2e"
  fi

  if ! run_cli templates list >/dev/null 2>&1; then
    fail "Bastion API is not reachable on $API_URL; run mise dev:up"
  fi

  if ! "$BASTION" system --data-dir "$DATA_DIR" check >/dev/null 2>&1; then
    fail "Bastion system check is not ok for $DATA_DIR; run bastion system --data-dir '$DATA_DIR' add cloud-hypervisor"
  fi
}

json_get() {
  jq -r "$1"
}

mux_template_config() {
  jq -nc --arg run_id "$RUN_ID" '{
    actions: {
      init: [
        {run: "set -eu\nmkdir -p /opt/bastion-mux-e2e\nprintf \"%s\\n\" \"\($run_id)\" > /opt/bastion-mux-e2e/init-run"}
      ]
    }
  }'
}

create_environment() {
  local output

  TEMPLATE_KEY="$RUN_ID-template"
  log "creating template $TEMPLATE_KEY"
  run_cli templates create --key "$TEMPLATE_KEY" --config "$(mux_template_config)" >/dev/null

  log "creating environment from $TEMPLATE_KEY"
  output="$(run_cli env create --template-key "$TEMPLATE_KEY")"
  ENV_ID="$(json_get '.id' <<<"$output")"

  if [ -z "$ENV_ID" ] || [ "$ENV_ID" = "null" ]; then
    fail "environment from $TEMPLATE_KEY did not return an id"
  fi

  if [ "$(json_get '.status' <<<"$output")" != "running" ]; then
    fail "environment $ENV_ID is not running: $output"
  fi
}

open_mux_session() {
  log "opening mux session $MUX_SESSION for $ENV_ID"

  tmux new-session -d -s "$CONTROL_SESSION" -n tui "BASTION_MUX_SESSION=$MUX_SESSION $BASTION --api-url $API_URL mux"
  sleep 1
  tmux send-keys -t "$CONTROL_SESSION" n
  wait_for_control_text "$ENV_ID"
  tmux send-keys -t "$CONTROL_SESSION" Enter
  wait_for_mux_window
  tmux send-keys -t "$CONTROL_SESSION" C-b d
}

wait_for_control_text() {
  local text=$1
  local output

  for _ in {1..60}; do
    output="$(tmux capture-pane -p -t "$CONTROL_SESSION" 2>/dev/null || true)"
    if [[ "$output" == *"$text"* ]]; then
      return
    fi

    sleep 1
  done

  tmux capture-pane -p -t "$CONTROL_SESSION" >/tmp/opencode/bastion-logs/mux-test-ui.log 2>/dev/null || true
  fail "mux picker did not show $text; see /tmp/opencode/bastion-logs/mux-test-ui.log"
}

wait_for_mux_window() {
  for _ in {1..60}; do
    if [ -n "$(tmux_mux_pane 2>/dev/null || true)" ]; then
      return
    fi

    sleep 1
  done

  tmux capture-pane -p -t "$CONTROL_SESSION" >/tmp/opencode/bastion-logs/mux-test-ui.log 2>/dev/null || true
  fail "mux did not create a tmux SSH window for $ENV_ID; see /tmp/opencode/bastion-logs/mux-test-ui.log"
}

tmux_mux_pane() {
  local format

  format=$'#{pane_id}\t#{@bastion_env_id}'
  tmux list-windows -t "$MUX_SESSION" -F "$format" | jq -Rsr --arg env_id "$ENV_ID" 'split("\n") | map(split("\t")) | map(select(.[1] == $env_id)) | .[0][0] // ""'
}

assert_mux_session_persisted() {
  local pane

  if ! tmux has-session -t "$MUX_SESSION" >/dev/null 2>&1; then
    fail "tmux session $MUX_SESSION was not left running after detach"
  fi

  pane="$(tmux_mux_pane)"
  if [ -z "$pane" ]; then
    fail "tmux session $MUX_SESSION does not have a window for $ENV_ID"
  fi

  log "sending command through persisted tmux pane $pane"
  tmux send-keys -t "$pane" "printf %s '$RUN_ID' > /opt/bastion-mux-e2e/persisted" Enter
  sleep 2

  run_cli ssh --id "$ENV_ID" -- grep -q "$RUN_ID" /opt/bastion-mux-e2e/persisted
}

main() {
  precheck
  trap cleanup EXIT

  log "starting mux e2e run $RUN_ID"
  create_environment
  open_mux_session
  assert_mux_session_persisted
  log "mux e2e run passed"
}

main "$@"
