#!/usr/bin/env sh
set -eu

auth="${BASTION_INPUT_AUTH:-}"
config="${BASTION_INPUT_CONFIG:-}"
tui="${BASTION_INPUT_TUI:-}"

if [ -z "$tui" ]; then
  tui='{"mouse":false,"keybinds":{"input_paste":"none"}}'
fi

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends bash ca-certificates curl jq tar gzip

curl -fsSL https://opencode.ai/install | bash -s -- --no-modify-path

if [ ! -x /root/.opencode/bin/opencode ]; then
  printf 'OpenCode installer did not create /root/.opencode/bin/opencode\n' >&2
  exit 1
fi

ln -sf /root/.opencode/bin/opencode /usr/local/bin/opencode

umask 077
config_dir=/root/.config/opencode
data_dir=/root/.local/share/opencode
config_tmp=$(mktemp)
auth_tmp=$(mktemp)
tui_tmp=$(mktemp)
trap 'rm -f "$config_tmp" "$auth_tmp" "$tui_tmp"' EXIT

printf '%s' "$tui" | jq -e 'if type == "object" then . else error("tui must be a JSON object") end' > "$tui_tmp"
mkdir -p "$config_dir"
install -m 600 "$tui_tmp" "$config_dir/tui.json"

if [ -n "$config" ]; then
  printf '%s' "$config" | jq -e 'if type == "object" then . else error("config must be a JSON object") end' > "$config_tmp"
  install -m 600 "$config_tmp" "$config_dir/opencode.json"
fi

if [ -n "$auth" ]; then
  printf '%s' "$auth" | jq -e 'if type == "object" then . else error("auth must be a JSON object") end' > "$auth_tmp"
  mkdir -p "$data_dir"
  install -m 600 "$auth_tmp" "$data_dir/auth.json"
fi

/usr/local/bin/opencode --version

if [ -n "$config" ]; then
  jq -e 'type == "object"' "$config_dir/opencode.json" >/dev/null
fi

jq -e 'type == "object"' "$config_dir/tui.json" >/dev/null

if [ -n "$auth" ]; then
  jq -e 'type == "object"' "$data_dir/auth.json" >/dev/null
fi
