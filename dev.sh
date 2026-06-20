#!/usr/bin/env bash
set -euo pipefail

command="${1:-up}"
session="${BASTION_DEV_SESSION:-bastion-dev}"
root="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"

require_tmux() {
  if command -v tmux >/dev/null 2>&1; then
    return
  fi
  printf 'tmux is not available. Run mise install and try again.\n' >&2
  exit 1
}

require_docker_compose() {
  if docker compose version >/dev/null 2>&1; then
    return
  fi
  printf 'docker compose is not available. Install Docker and try again.\n' >&2
  exit 1
}

dev_infra_up() {
  require_docker_compose
  docker compose -f "$root/compose.yml" up -d postgres minio minio-init >/dev/null
}

dev_infra_down() {
  if docker compose version >/dev/null 2>&1; then
    docker compose -f "$root/compose.yml" down
  fi
}

dev_vm_network_prefix() {
  if [ -n "${BASTION_VM_NETWORK_PREFIX:-}" ]; then
    printf '%s\n' "$BASTION_VM_NETWORK_PREFIX"
    return
  fi

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

  printf '10.241\n'
}

attach_session() {
  if [ -n "${TMUX:-}" ]; then
    tmux switch-client -t "$session"
  else
    tmux attach-session -t "$session"
  fi
}

dev_up() {
  local vm_network_prefix

  if [ -z "${TMUX:-}" ] && { [ ! -t 0 ] || [ ! -t 1 ]; }; then
    printf 'mise run dev:up requires an interactive terminal so tmux can attach.\n' >&2
    exit 1
  fi

  require_tmux

  dev_infra_up

  vm_network_prefix="$(dev_vm_network_prefix)"

  if tmux has-session -t "$session" 2>/dev/null; then
    attach_session
    return
  fi

  api_pane="$(tmux new-session -d -P -F '#{pane_id}' -s "$session" -n dev -c "$root" 'mise run //core:dev:api')"
  bastiond_pane="$(tmux split-window -d -h -P -F '#{pane_id}' -t "$api_pane" -c "$root" "BASTION_VM_NETWORK_PREFIX='$vm_network_prefix' sudo -E mise run //core:dev:daemon")"
  cluster_pane="$(tmux split-window -d -v -P -F '#{pane_id}' -t "$api_pane" -c "$root" 'mise run //core:dev:cluster')"
  drizzle_node_pane="$(tmux split-window -d -v -P -F '#{pane_id}' -t "$bastiond_pane" -c "$root" 'mise run //.dev/drizzle:dev:node')"
  drizzle_cluster_pane="$(tmux split-window -d -v -P -F '#{pane_id}' -t "$cluster_pane" -c "$root" 'mise run //.dev/drizzle:dev:cluster')"
  docs_pane="$(tmux split-window -d -v -P -F '#{pane_id}' -t "$api_pane" -c "$root" 'mise run //docs:dev')"
  shell_pane="$(tmux split-window -d -v -P -F '#{pane_id}' -t "$drizzle_node_pane" -c "$root" 'mise exec -- bash -l')"

  tmux select-pane -t "$api_pane" -T 'api'
  tmux select-pane -t "$bastiond_pane" -T 'bastiond'
  tmux select-pane -t "$cluster_pane" -T 'cluster'
  tmux select-pane -t "$drizzle_node_pane" -T 'drizzle-node'
  tmux select-pane -t "$drizzle_cluster_pane" -T 'drizzle-cluster'
  tmux select-pane -t "$docs_pane" -T 'docs'
  tmux select-pane -t "$shell_pane" -T 'shell'

  tmux set-window-option -t "$session:dev" pane-border-status top >/dev/null
  tmux set-window-option -t "$session:dev" pane-border-format ' #{pane_index}: #{pane_title} ' >/dev/null
  tmux set-window-option -t "$session:dev" remain-on-exit on >/dev/null
  tmux select-layout -t "$session:dev" tiled >/dev/null
  tmux select-pane -t "$shell_pane"

  attach_session
}

dev_down() {
  require_tmux

  if ! tmux has-session -t "$session" 2>/dev/null; then
    printf 'No tmux session named %s is running.\n' "$session"
    dev_infra_down
    return
  fi

  current_session=""
  if [ -n "${TMUX:-}" ]; then
    current_session="$(tmux display-message -p '#S' 2>/dev/null || true)"
  fi

  if [ "$current_session" = "$session" ]; then
    printf 'Stopping tmux session %s.\n' "$session"
    nohup bash -c 'sleep 0.1; tmux kill-session -t "$1"; docker compose -f "$2/compose.yml" down' bash "$session" "$root" >/dev/null 2>&1 &
    return
  fi

  tmux kill-session -t "$session"
  dev_infra_down
  printf 'Stopped tmux session %s.\n' "$session"
}

case "$command" in
up)
  dev_up
  ;;
down)
  dev_down
  ;;
*)
  printf 'Usage: %s [up|down]\n' "$0" >&2
  exit 2
  ;;
esac
