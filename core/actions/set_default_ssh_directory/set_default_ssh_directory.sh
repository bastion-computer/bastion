#!/usr/bin/env sh
set -eu

target="${BASTION_INPUT_PATH:-}"
if [ -z "$target" ]; then
  printf 'BASTION_INPUT_PATH is required\n' >&2
  exit 2
fi

case "$target" in
  *'
'*)
    printf 'Default SSH directory path must not contain newlines\n' >&2
    exit 2
    ;;
esac

mkdir -p /etc/bastion
printf '%s\n' "$target" > /etc/bastion/default-ssh-directory
chmod 600 /etc/bastion/default-ssh-directory

bashrc=/root/.bashrc
start_marker='# >>> bastion default ssh directory >>>'
end_marker='# <<< bastion default ssh directory <<<'
tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

touch "$bashrc"
awk -v start="$start_marker" -v end="$end_marker" '
  $0 == start { skip = 1; next }
  $0 == end { skip = 0; next }
  !skip { print }
' "$bashrc" > "$tmp"

{
  printf '\n%s\n' "$start_marker"
  printf 'if [ -r /etc/bastion/default-ssh-directory ]; then\n'
  printf '  IFS= read -r bastion_default_ssh_directory < /etc/bastion/default-ssh-directory || bastion_default_ssh_directory=\n'
  printf '  if [ -n "$bastion_default_ssh_directory" ] && [ -d "$bastion_default_ssh_directory" ]; then\n'
  printf '    cd "$bastion_default_ssh_directory"\n'
  printf '  fi\n'
  printf '  unset bastion_default_ssh_directory\n'
  printf 'fi\n'
  printf '%s\n' "$end_marker"
} >> "$tmp"

cp "$tmp" "$bashrc"
