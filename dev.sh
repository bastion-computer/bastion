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

attach_session() {
  if [ -n "${TMUX:-}" ]; then
    tmux switch-client -t "$session"
  else
    tmux attach-session -t "$session"
  fi
}

dev_up() {
  if [ -z "${TMUX:-}" ] && { [ ! -t 0 ] || [ ! -t 1 ]; }; then
    printf 'mise run dev:up requires an interactive terminal so tmux can attach.\n' >&2
    exit 1
  fi

  require_tmux

  if tmux has-session -t "$session" 2>/dev/null; then
    attach_session
    return
  fi

  core_pane="$(tmux new-session -d -P -F '#{pane_id}' -s "$session" -n dev -c "$root" 'mise run //core:dev')"
  drizzle_pane="$(tmux split-window -d -h -P -F '#{pane_id}' -t "$core_pane" -c "$root" 'mise run //.dev/drizzle:dev')"
  docs_pane="$(tmux split-window -d -v -P -F '#{pane_id}' -t "$core_pane" -c "$root" 'mise run //docs:dev')"
  shell_pane="$(tmux split-window -d -v -P -F '#{pane_id}' -t "$drizzle_pane" -c "$root" 'mise exec -- bash -l')"

  tmux select-pane -t "$core_pane" -T 'core'
  tmux select-pane -t "$drizzle_pane" -T 'drizzle'
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
    return
  fi

  current_session=""
  if [ -n "${TMUX:-}" ]; then
    current_session="$(tmux display-message -p '#S' 2>/dev/null || true)"
  fi

  if [ "$current_session" = "$session" ]; then
    printf 'Stopping tmux session %s.\n' "$session"
    nohup bash -c 'sleep 0.1; tmux kill-session -t "$1"' bash "$session" >/dev/null 2>&1 &
    return
  fi

  tmux kill-session -t "$session"
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
