#!/usr/bin/env sh
set -eu

export DEBIAN_FRONTEND=noninteractive

apt-get update
apt-get install -y --no-install-recommends ca-certificates curl

conflicting_packages=""
for package in docker.io docker-compose docker-compose-v2 docker-doc podman-docker containerd runc; do
  if dpkg-query -W -f='${Status}' "$package" 2>/dev/null | grep -q 'install ok installed'; then
    conflicting_packages="$conflicting_packages $package"
  fi
done

if [ -n "$conflicting_packages" ]; then
  apt-get remove -y $conflicting_packages
fi

install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
chmod a+r /etc/apt/keyrings/docker.asc

suite="$(. /etc/os-release && printf '%s' "${UBUNTU_CODENAME:-$VERSION_CODENAME}")"
if [ -z "$suite" ]; then
  printf 'Ubuntu codename is required to configure the Docker apt repository\n' >&2
  exit 2
fi

arch="$(dpkg --print-architecture)"
cat > /etc/apt/sources.list.d/docker.sources <<EOF
Types: deb
URIs: https://download.docker.com/linux/ubuntu
Suites: $suite
Components: stable
Architectures: $arch
Signed-By: /etc/apt/keyrings/docker.asc
EOF

apt-get update
apt-get install -y --no-install-recommends docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

systemctl enable --now docker

i=0
while [ "$i" -lt 30 ]; do
  if docker info >/dev/null 2>&1; then
    break
  fi

  i=$((i + 1))
  sleep 1
done

docker --version
docker buildx version
docker compose version
docker info >/dev/null
