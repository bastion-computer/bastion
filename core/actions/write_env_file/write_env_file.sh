#!/usr/bin/env sh
set -eu

target_dir="${BASTION_INPUT_PATH:-}"
context_file="${BASTION_CONTEXT_FILE:-}"

if [ -z "$target_dir" ]; then
  printf 'BASTION_INPUT_PATH is required\n' >&2
  exit 2
fi

case "$target_dir" in
  *'
'*)
    printf 'Env file target directory must not contain newlines\n' >&2
    exit 2
    ;;
esac

if [ -z "$context_file" ]; then
  printf 'BASTION_CONTEXT_FILE is required\n' >&2
  exit 2
fi

if [ ! -r "$context_file" ]; then
  printf 'BASTION_CONTEXT_FILE is not readable: %s\n' "$context_file" >&2
  exit 2
fi

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

mkdir -p "$target_dir"

jq -r '
def env_value:
  if type == "string" then .
  elif type == "number" or type == "boolean" then tostring
  elif type == "null" then ""
  else tojson
  end;

if type != "object" then
  error("write_env_file context must be a JSON object")
else
  to_entries
  | sort_by(.key)
  | .[]
  | if (.key | test("^[A-Za-z_][A-Za-z0-9_]*$")) then
      "\(.key)=\(.value | env_value | @sh)"
    else
      error("invalid environment variable name: \(.key)")
    end
end
' "$context_file" > "$tmp"

install -m 600 "$tmp" "$target_dir/.env"
