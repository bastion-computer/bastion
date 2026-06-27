#!/usr/bin/env bash
set -euo pipefail

WORKFLOW="${1:-.github/workflows/release.yml}"
TMP_DIR=""

fail() {
  printf 'release workflow check failed: %s\n' "$*" >&2
  exit 1
}

cleanup() {
  if [ -n "$TMP_DIR" ]; then
    rm -rf "$TMP_DIR"
  fi
}
trap cleanup EXIT

matrix_binaries() {
  local target=$1
  local current=""
  local line

  while IFS= read -r line; do
    line="${line#"${line%%[![:space:]]*}"}"
    case "$line" in
      '- name: '*)
        current="${line#- name: }"
        ;;
      'binaries: '*)
        if [ "$current" = "$target" ]; then
          printf '%s\n' "${line#binaries: }"
          return 0
        fi
        ;;
    esac
  done <"$WORKFLOW"

  return 1
}

make_fake_binary() {
  local path=$1

  if [ -e "$path" ]; then
    return
  fi

  printf '#!/usr/bin/env sh\nexit 0\n' >"$path"
  chmod 0755 "$path"
}

check_target() {
  local target=$1
  local expected=$2
  local actual
  local target_dir
  local archive
  local binary

  actual="$(matrix_binaries "$target")" || fail "missing $target matrix binaries"
  if [ "$actual" != "$expected" ]; then
    fail "$target matrix binaries are '$actual', want '$expected'"
  fi

  target_dir="$TMP_DIR/$target"
  archive="$target_dir/dist/bastion_test_${target}.tar.gz"

  mkdir -p "$target_dir/tmp" "$target_dir/dist" "$target_dir/extract"
  for binary in $expected $actual; do
    make_fake_binary "$target_dir/tmp/$binary"
  done

  tar -C "$target_dir/tmp" -czf "$archive" $actual
  tar -xzf "$archive" -C "$target_dir/extract"

  for binary in $expected; do
    if [ ! -x "$target_dir/extract/$binary" ]; then
      fail "$target archive is missing executable $binary (matrix binaries: $actual)"
    fi
  done
}

check_darwin_client_build() {
  (
    cd core
    BASTION_VERSION=test CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 \
      go build \
        -ldflags '-X github.com/bastion-computer/bastion/core/internal/config.Version=test' \
        -o "$TMP_DIR/bastion_darwin_arm64" \
        ./cmd/bastion
  )
}

check_release_channel() {
  if ! grep -q 'id: release_channel' "$WORKFLOW"; then
    fail "workflow does not resolve the GitHub release channel"
  fi

  if ! grep -q '\*-rc\.\*' "$WORKFLOW"; then
    fail "workflow does not detect -rc.* tags as prereleases"
  fi

  if ! grep -q 'prerelease:.*steps\.release_channel\.outputs\.prerelease' "$WORKFLOW"; then
    fail "GitHub release prerelease flag is not driven by the release channel"
  fi

  if ! grep -q 'make_latest:.*steps\.release_channel\.outputs\.make_latest' "$WORKFLOW"; then
    fail "GitHub release latest flag is not driven by the release channel"
  fi
}

check_docker_publish() {
  if [ ! -f Dockerfile ]; then
    fail "Dockerfile is required for Docker Hub release images"
  fi

  if ! grep -q 'docker/build-push-action@' "$WORKFLOW"; then
    fail "workflow does not use docker/build-push-action"
  fi

  if ! grep -q 'docker/login-action@' "$WORKFLOW"; then
    fail "workflow does not log in to Docker Hub"
  fi

  if ! grep -q 'DOCKERHUB_USERNAME' "$WORKFLOW"; then
    fail "workflow does not use DOCKERHUB_USERNAME"
  fi

  if ! grep -q 'DOCKERHUB_TOKEN' "$WORKFLOW"; then
    fail "workflow does not use DOCKERHUB_TOKEN"
  fi

  if ! grep -q 'linux/amd64,linux/arm64' "$WORKFLOW"; then
    fail "workflow does not build linux/amd64 and linux/arm64 images"
  fi

  if ! grep -q 'bastioncomputer/bastion' "$WORKFLOW"; then
    fail "workflow does not publish to bastioncomputer/bastion"
  fi

  if ! grep -q 'BASTION_VERSION=.*github.ref_name' "$WORKFLOW"; then
    fail "workflow does not pass the release tag into the Docker build"
  fi

  if ! grep -q 'latest.*release_channel\.outputs\.make_latest' "$WORKFLOW"; then
    fail "workflow does not gate the latest Docker tag on the release channel"
  fi
}

if [ ! -f "$WORKFLOW" ]; then
  fail "workflow not found: $WORKFLOW"
fi

TMP_DIR="$(mktemp -d)"
check_target linux_x86_64 'bastion bastion-guest-proxy'
check_target darwin_arm64 'bastion'
check_release_channel
check_docker_publish
check_darwin_client_build

printf 'release workflow package contents, docker publishing, and darwin client build are valid\n'
