#!/usr/bin/env sh
set -eu

provider="${BASTION_INPUT_PROVIDER:-}"
model="${BASTION_INPUT_MODEL:-}"
api_key="${BASTION_INPUT_API_KEY:-}"
small_model="${BASTION_INPUT_SMALL_MODEL:-}"
base_url="${BASTION_INPUT_BASE_URL:-}"
share="${BASTION_INPUT_SHARE:-}"
permission="${BASTION_INPUT_PERMISSION:-}"
version="${BASTION_INPUT_VERSION:-}"
extra_config="${BASTION_INPUT_CONFIG:-}"

if [ -z "$provider" ]; then
  printf 'BASTION_INPUT_PROVIDER is required\n' >&2
  exit 2
fi

if [ -z "$model" ]; then
  printf 'BASTION_INPUT_MODEL is required\n' >&2
  exit 2
fi

if [ -z "$api_key" ]; then
  printf 'BASTION_INPUT_API_KEY is required\n' >&2
  exit 2
fi

case "$share" in
  ''|manual|auto|disabled)
    ;;
  *)
    printf 'OpenCode share must be manual, auto, or disabled\n' >&2
    exit 2
    ;;
esac

case "$permission" in
  ''|ask|allow|deny)
    ;;
  *)
    printf 'OpenCode permission must be ask, allow, or deny\n' >&2
    exit 2
    ;;
esac

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends bash ca-certificates curl jq tar gzip

if [ -n "$version" ]; then
  case "$version" in
    *[!A-Za-z0-9._-]*)
      printf 'OpenCode version contains unsupported characters\n' >&2
      exit 2
      ;;
  esac

  curl -fsSL https://opencode.ai/install | bash -s -- --no-modify-path --version "$version"
else
  curl -fsSL https://opencode.ai/install | bash -s -- --no-modify-path
fi

if [ ! -x /root/.opencode/bin/opencode ]; then
  printf 'OpenCode installer did not create /root/.opencode/bin/opencode\n' >&2
  exit 1
fi

ln -sf /root/.opencode/bin/opencode /usr/local/bin/opencode

umask 077
config_dir=/root/.config/opencode
data_dir=/root/.local/share/opencode
mkdir -p "$config_dir"
mkdir -p "$data_dir"
base_config=$(mktemp)
merged_config=$(mktemp)
auth_config=$(mktemp)
trap 'rm -f "$base_config" "$merged_config" "$auth_config"' EXIT

jq -n \
  --arg provider "$provider" \
  --arg model "$model" \
  --arg api_key "$api_key" \
  --arg small_model "$small_model" \
  --arg base_url "$base_url" \
  --arg share "$share" \
  --arg permission "$permission" \
  '{
    "$schema": "https://opencode.ai/config.json",
    model: $model,
    provider: {
      ($provider): {
        options: (if $base_url != "" then {baseURL: $base_url} else {} end)
      }
    }
  }
  + (if $small_model != "" then {small_model: $small_model} else {} end)
  + (if $share != "" then {share: $share} else {} end)
  + (if $permission != "" then {permission: $permission} else {} end)' \
  > "$base_config"

if [ -n "$extra_config" ]; then
  printf '%s' "$extra_config" | jq -e 'type == "object"' >/dev/null
  printf '%s' "$extra_config" | jq -s '.[0] * .[1]' "$base_config" - > "$merged_config"
else
  cp "$base_config" "$merged_config"
fi

install -m 600 "$merged_config" "$config_dir/opencode.json"

auth_file="$data_dir/auth.json"
if [ -s "$auth_file" ]; then
  jq --arg provider "$provider" --arg api_key "$api_key" \
    '. + {($provider): {type: "api", key: $api_key}}' \
    "$auth_file" > "$auth_config"
else
  jq -n --arg provider "$provider" --arg api_key "$api_key" \
    '{($provider): {type: "api", key: $api_key}}' \
    > "$auth_config"
fi
install -m 600 "$auth_config" "$auth_file"

/usr/local/bin/opencode --version
jq -e \
  --arg model "$model" \
  '.model == $model' \
  "$config_dir/opencode.json" >/dev/null
jq -e \
  --arg provider "$provider" \
  '.[$provider].type == "api" and (.[$provider].key | type == "string" and length > 0)' \
  "$auth_file" >/dev/null
