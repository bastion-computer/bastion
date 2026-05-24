#!/usr/bin/env sh
set -eu

version="${BASTION_INPUT_VERSION:-}"

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends ca-certificates curl

mkdir -p /usr/local/bin
export MISE_INSTALL_PATH=/usr/local/bin/mise
if [ -n "$version" ]; then
  export MISE_VERSION="$version"
fi

curl -fsSL https://mise.run | sh

activation='eval "$(/usr/local/bin/mise activate bash)"'
touch /root/.bashrc
if ! grep -Fxq "$activation" /root/.bashrc; then
  printf '\n%s\n' "$activation" >> /root/.bashrc
fi

/usr/local/bin/mise --version
