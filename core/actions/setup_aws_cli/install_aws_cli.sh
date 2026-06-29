#!/usr/bin/env sh
set -eu

access_key_id="${BASTION_INPUT_ACCESS_KEY_ID:-}"
secret_access_key="${BASTION_INPUT_SECRET_ACCESS_KEY:-}"
region="${BASTION_INPUT_REGION:-}"
session_token="${BASTION_INPUT_SESSION_TOKEN:-}"
profile="${BASTION_INPUT_PROFILE:-default}"
output="${BASTION_INPUT_OUTPUT:-json}"

require_input() {
  name="$1"
  value="$2"

  if [ -z "$value" ]; then
    printf '%s is required\n' "$name" >&2
    exit 2
  fi
}

reject_newline() {
  name="$1"
  value="$2"

  case "$value" in
    *'
'*)
      printf '%s must not contain newlines\n' "$name" >&2
      exit 2
      ;;
  esac
}

require_input BASTION_INPUT_ACCESS_KEY_ID "$access_key_id"
require_input BASTION_INPUT_SECRET_ACCESS_KEY "$secret_access_key"
require_input BASTION_INPUT_REGION "$region"
require_input BASTION_INPUT_PROFILE "$profile"
require_input BASTION_INPUT_OUTPUT "$output"

reject_newline BASTION_INPUT_ACCESS_KEY_ID "$access_key_id"
reject_newline BASTION_INPUT_SECRET_ACCESS_KEY "$secret_access_key"
reject_newline BASTION_INPUT_REGION "$region"
reject_newline BASTION_INPUT_SESSION_TOKEN "$session_token"
reject_newline BASTION_INPUT_PROFILE "$profile"
reject_newline BASTION_INPUT_OUTPUT "$output"

case "$output" in
  json|yaml|yaml-stream|text|table|off)
    ;;
  *)
    printf 'AWS CLI output must be json, yaml, yaml-stream, text, table, or off\n' >&2
    exit 2
    ;;
esac

case "$(uname -m)" in
  x86_64|amd64)
    installer_arch="x86_64"
    ;;
  aarch64|arm64)
    installer_arch="aarch64"
    ;;
  *)
    printf 'Unsupported AWS CLI installer architecture: %s\n' "$(uname -m)" >&2
    exit 2
    ;;
esac

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends ca-certificates curl groff less unzip

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

curl -fsSL "https://awscli.amazonaws.com/awscli-exe-linux-$installer_arch.zip" \
  -o "$tmp_dir/awscliv2.zip"
unzip -q "$tmp_dir/awscliv2.zip" -d "$tmp_dir"

install_args="--bin-dir /usr/local/bin --install-dir /usr/local/aws-cli"
if [ -x /usr/local/aws-cli/v2/current/bin/aws ]; then
  # The official installer requires --update when replacing an existing install.
  sh "$tmp_dir/aws/install" $install_args --update
else
  sh "$tmp_dir/aws/install" $install_args
fi

aws configure set aws_access_key_id "$access_key_id" --profile "$profile"
aws configure set aws_secret_access_key "$secret_access_key" --profile "$profile"
if [ -n "$session_token" ]; then
  aws configure set aws_session_token "$session_token" --profile "$profile"
fi
aws configure set region "$region" --profile "$profile"
aws configure set output "$output" --profile "$profile"

chmod 700 /root/.aws
if [ -f /root/.aws/config ]; then
  chmod 600 /root/.aws/config
fi
if [ -f /root/.aws/credentials ]; then
  chmod 600 /root/.aws/credentials
fi

aws --version
aws configure get region --profile "$profile" >/dev/null
aws configure get aws_access_key_id --profile "$profile" >/dev/null
