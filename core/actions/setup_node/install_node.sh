#!/usr/bin/env sh
set -eu

version="${BASTION_INPUT_VERSION:-}"
if [ -z "$version" ]; then
  printf 'BASTION_INPUT_VERSION is required\n' >&2
  exit 2
fi

major="${version%%.*}"
case "$major" in
  ''|*[!0-9]*)
    printf 'Node.js version must start with a numeric major version: %s\n' "$version" >&2
    exit 2
    ;;
esac

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends ca-certificates curl gnupg

mkdir -p /etc/apt/keyrings
rm -f /etc/apt/keyrings/nodesource.gpg
curl -fsSL https://deb.nodesource.com/gpgkey/nodesource-repo.gpg.key \
  | gpg --dearmor -o /etc/apt/keyrings/nodesource.gpg

printf 'deb [signed-by=/etc/apt/keyrings/nodesource.gpg] https://deb.nodesource.com/node_%s.x nodistro main\n' "$major" \
  > /etc/apt/sources.list.d/nodesource.list

apt-get update
apt-get install -y --no-install-recommends nodejs
node --version
