#!/usr/bin/env sh
set -eu

name="${BASTION_INPUT_NAME:-}"
command="${BASTION_INPUT_COMMAND:-}"
working_directory="${BASTION_INPUT_WORKING_DIRECTORY:-/root}"
description="${BASTION_INPUT_DESCRIPTION:-}"
restart="${BASTION_INPUT_RESTART:-always}"
user="${BASTION_INPUT_USER:-root}"
health_url="${BASTION_INPUT_HEALTH_URL:-}"
timeout_seconds="${BASTION_INPUT_TIMEOUT_SECONDS:-60}"
start="${BASTION_INPUT_START:-true}"

fail_validation() {
  printf '%s\n' "$1" >&2
  exit 2
}

reject_newline() {
  label=$1
  value=$2

  case "$value" in
    *'
'*)
      fail_validation "$label must not contain newlines"
      ;;
  esac
}

systemd_escape() {
  value=$1

  while [ -n "$value" ]; do
    char=${value%"${value#?}"}
    value=${value#?}

    case "$char" in
      '%')
        printf '%s' '%%'
        ;;
      \\)
        printf '%s' '\\'
        ;;
      ' ')
        printf '%s' '\x20'
        ;;
      '"')
        printf '%s' '\x22'
        ;;
      "'")
        printf '%s' '\x27'
        ;;
      '	')
        printf '%s' '\x09'
        ;;
      *)
        printf '%s' "$char"
        ;;
    esac
  done
}

systemd_description_escape() {
  value=$1

  while [ -n "$value" ]; do
    char=${value%"${value#?}"}
    value=${value#?}

    case "$char" in
      '%')
        printf '%s' '%%'
        ;;
      \\)
        printf '%s' '\\'
        ;;
      '"')
        printf '%s' '\x22'
        ;;
      "'")
        printf '%s' '\x27'
        ;;
      '	')
        printf '%s' '\x09'
        ;;
      *)
        printf '%s' "$char"
        ;;
    esac
  done
}

exec_shell_quote() {
  value=$1

  printf "'"
  while [ -n "$value" ]; do
    char=${value%"${value#?}"}
    value=${value#?}

    case "$char" in
      '%')
        printf '%s' '%%'
        ;;
      \\)
        printf '%s' '\\'
        ;;
      "'")
        printf '%s' "'\\''"
        ;;
      *)
        printf '%s' "$char"
        ;;
    esac
  done
  printf "'"
}

case "$name" in
  ''|[!A-Za-z]*)
    fail_validation 'Service name must start with a letter and contain only letters, numbers, underscores, and dashes'
    ;;
esac

case "$name" in
  *[!A-Za-z0-9_-]*)
    fail_validation 'Service name must be provided without .service and contain only letters, numbers, underscores, and dashes'
    ;;
esac

if [ -z "$command" ]; then
  fail_validation 'Command is required'
fi
reject_newline 'Command' "$command"

case "$working_directory" in
  /*)
    ;;
  *)
    fail_validation 'Working directory must be absolute'
    ;;
esac
reject_newline 'Working directory' "$working_directory"

if [ -z "$description" ]; then
  description="Bastion managed service $name"
fi
reject_newline 'Description' "$description"

case "$restart" in
  no|on-failure|always)
    ;;
  *)
    fail_validation 'Restart must be one of no, on-failure, or always'
    ;;
esac

case "$user" in
  ''|[!A-Za-z_]*)
    fail_validation 'User must start with a letter or underscore and contain only letters, numbers, underscores, dashes, and an optional trailing dollar sign'
    ;;
esac

user_without_suffix=$user
case "$user_without_suffix" in
  *'$')
    user_without_suffix=${user_without_suffix%?}
    ;;
esac

case "$user_without_suffix" in
  *[!A-Za-z0-9_-]*)
    fail_validation 'User must start with a letter or underscore and contain only letters, numbers, underscores, dashes, and an optional trailing dollar sign'
    ;;
esac

if [ -n "$health_url" ]; then
  reject_newline 'Health URL' "$health_url"
  case "$health_url" in
    http://*|https://*)
      ;;
    *)
      fail_validation 'Health URL must start with http:// or https://'
      ;;
  esac
fi

case "$timeout_seconds" in
  ''|*[!0-9]*)
    fail_validation 'Timeout seconds must be a positive integer'
    ;;
  *)
    if [ "$timeout_seconds" -le 0 ]; then
      fail_validation 'Timeout seconds must be a positive integer'
    fi
    ;;
esac

case "$start" in
  true|false)
    ;;
  *)
    fail_validation 'Start must be true or false'
    ;;
esac

unit="$name.service"
unit_path="/etc/systemd/system/$unit"
tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

mkdir -p "$working_directory"

{
  printf '[Unit]\n'
  printf 'Description=%s\n' "$(systemd_description_escape "$description")"
  printf 'After=network-online.target\n'
  printf 'Wants=network-online.target\n'
  printf '\n'
  printf '[Service]\n'
  printf 'Type=simple\n'
  printf 'User=%s\n' "$user"
  printf 'WorkingDirectory=%s\n' "$(systemd_escape "$working_directory")"
  printf 'ExecStart=/bin/sh -lc %s\n' "$(exec_shell_quote "$command")"
  printf 'Restart=%s\n' "$restart"
  printf 'RestartSec=2\n'
  if [ "$user" = 'root' ]; then
    printf 'Environment=HOME=/root\n'
  fi
  printf '\n'
  printf '[Install]\n'
  printf 'WantedBy=multi-user.target\n'
} > "$tmp"

install -m 0644 "$tmp" "$unit_path"
systemctl daemon-reload
systemctl enable "$unit"

if [ "$start" = 'true' ]; then
  systemctl restart "$unit"

  if [ -n "$health_url" ]; then
    i=0
    while [ "$i" -lt "$timeout_seconds" ]; do
      if curl -fsS --connect-timeout 1 --max-time 2 "$health_url" >/dev/null 2>&1; then
        exit 0
      fi

      i=$((i + 1))
      sleep 1
    done

    systemctl status --no-pager "$unit" >&2 || true
    journalctl -u "$unit" --no-pager -n 50 >&2 || true
    exit 1
  fi
fi
