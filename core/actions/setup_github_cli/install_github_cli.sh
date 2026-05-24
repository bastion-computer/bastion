#!/usr/bin/env sh
set -eu

token="${BASTION_INPUT_TOKEN:-}"
hostname="${BASTION_INPUT_HOSTNAME:-github.com}"
git_protocol="${BASTION_INPUT_GIT_PROTOCOL:-https}"

if [ -z "$token" ]; then
  printf 'BASTION_INPUT_TOKEN is required\n' >&2
  exit 2
fi

case "$hostname" in
  ''|*[!A-Za-z0-9.-]*)
    printf 'GitHub hostname must contain only letters, numbers, dots, and dashes\n' >&2
    exit 2
    ;;
esac

case "$git_protocol" in
  https|ssh)
    ;;
  *)
    printf 'GitHub CLI git_protocol must be https or ssh\n' >&2
    exit 2
    ;;
esac

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends ca-certificates curl

mkdir -p -m 755 /etc/apt/keyrings /etc/apt/sources.list.d
curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
  -o /etc/apt/keyrings/githubcli-archive-keyring.gpg
chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg

arch=$(dpkg --print-architecture)
printf 'deb [arch=%s signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main\n' "$arch" \
  > /etc/apt/sources.list.d/github-cli.list

apt-get update
apt-get install -y --no-install-recommends gh

mkdir -p /etc/bastion
{
  printf "export GH_HOST='%s'\n" "$hostname"
  printf 'export GH_PROMPT_DISABLED=1\n'
  printf 'export GH_NO_UPDATE_NOTIFIER=1\n'
  printf 'export GH_NO_EXTENSION_UPDATE_NOTIFIER=1\n'
} > /etc/bastion/github-cli.env
chmod 600 /etc/bastion/github-cli.env
printf '%s\n' "$token" > /etc/bastion/github-token
chmod 600 /etc/bastion/github-token

cat > /usr/local/bin/gh <<'EOF'
#!/usr/bin/env sh
if [ -r /etc/bastion/github-cli.env ]; then
  . /etc/bastion/github-cli.env
fi
if [ -r /etc/bastion/github-token ]; then
  IFS= read -r token < /etc/bastion/github-token || token=''
  export GH_TOKEN="$token"
  export GH_ENTERPRISE_TOKEN="$token"
fi
exec /usr/bin/gh "$@"
EOF
chmod 755 /usr/local/bin/gh

/usr/local/bin/gh config set git_protocol "$git_protocol" --host "$hostname" >/dev/null
/usr/local/bin/gh --version
/usr/local/bin/gh auth token --hostname "$hostname" >/dev/null
