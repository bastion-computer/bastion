#!/usr/bin/env sh
set -eu

api_level="${BASTION_INPUT_API_LEVEL:-}"
build_tools_version="${BASTION_INPUT_BUILD_TOOLS_VERSION:-}"
extra_packages="${BASTION_INPUT_EXTRA_PACKAGES:-}"
sdk_root="/opt/android-sdk"
repository_url="https://dl.google.com/android/repository/repository2-1.xml"
repository_base_url="https://dl.google.com/android/repository"

if ! command -v java >/dev/null 2>&1; then
  printf 'java is required for Android SDK command-line tools; run setup-openjdk before setup_android_sdk\n' >&2
  exit 2
fi

if [ -n "$api_level" ]; then
  case "$api_level" in
    ''|*[!0-9]*)
      printf 'Android API level must be an integer: %s\n' "$api_level" >&2
      exit 2
      ;;
  esac
fi

if [ -n "$build_tools_version" ]; then
  case "$build_tools_version" in
    ''|*[!0-9A-Za-z._-]*)
      printf 'Android Build Tools version must contain only letters, numbers, dots, underscores, or dashes: %s\n' "$build_tools_version" >&2
      exit 2
      ;;
  esac
fi

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends \
  ca-certificates \
  curl \
  libdbus-1-3 \
  libfontconfig1 \
  libgcc-s1 \
  libgl1 \
  libnss3 \
  libpulse0 \
  libstdc++6 \
  libx11-6 \
  libxkbcommon0 \
  libxkbcommon-x11-0 \
  libxcb1 \
  libxcb-cursor0 \
  libxcomposite1 \
  libxcursor1 \
  libxdamage1 \
  libxext6 \
  libxfixes3 \
  libxi6 \
  libxkbfile1 \
  libxrandr2 \
  libxrender1 \
  libxss1 \
  libxtst6 \
  unzip \
  zlib1g

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

curl -fsSL "$repository_url" -o "$tmp_dir/repository.xml"
cmdline_tools_zip="$(sed -n 's|.*<url>\(commandlinetools-linux-[0-9][0-9]*_latest\.zip\)</url>.*|\1|p' "$tmp_dir/repository.xml" | head -n 1)"
if [ -z "$cmdline_tools_zip" ]; then
  printf 'Could not find Android command-line tools download in %s\n' "$repository_url" >&2
  exit 2
fi

mkdir -p "$sdk_root/cmdline-tools"
curl -fsSL "$repository_base_url/$cmdline_tools_zip" -o "$tmp_dir/commandlinetools.zip"
unzip -q "$tmp_dir/commandlinetools.zip" -d "$tmp_dir"
rm -rf "$sdk_root/cmdline-tools/latest"
mv "$tmp_dir/cmdline-tools" "$sdk_root/cmdline-tools/latest"

sdkmanager="$sdk_root/cmdline-tools/latest/bin/sdkmanager"
avdmanager="$sdk_root/cmdline-tools/latest/bin/avdmanager"

mkdir -p /root/.android
: > /root/.android/repositories.cfg

if [ -z "$api_level" ]; then
  api_level="$($sdkmanager --sdk_root="$sdk_root" --list | sed -n 's/^ *platforms;android-\([0-9][0-9]*\) .*$/\1/p' | sort -n | tail -n 1)"
  if [ -z "$api_level" ]; then
    printf 'Could not determine the latest stable Android API level\n' >&2
    exit 2
  fi
fi

if [ -z "$build_tools_version" ]; then
  build_tools_version="$($sdkmanager --sdk_root="$sdk_root" --list | sed -n 's/^ *build-tools;\([0-9][0-9.]*\) .*$/\1/p' | sort -V | tail -n 1)"
  if [ -z "$build_tools_version" ]; then
    printf 'Could not determine the latest stable Android Build Tools version\n' >&2
    exit 2
  fi
fi

yes | "$sdkmanager" --sdk_root="$sdk_root" --licenses >/dev/null
yes | "$sdkmanager" --sdk_root="$sdk_root" \
  "platform-tools" \
  "emulator" \
  "platforms;android-$api_level" \
  "build-tools;$build_tools_version"

set -f
for package in $extra_packages; do
  yes | "$sdkmanager" --sdk_root="$sdk_root" "$package"
done
set +f

tmp_environment="$tmp_dir/environment"
if [ -f /etc/environment ]; then
  grep -v '^ANDROID_HOME=' /etc/environment | grep -v '^ANDROID_SDK_ROOT=' > "$tmp_environment" || true
fi
{
  printf 'ANDROID_HOME="%s"\n' "$sdk_root"
  printf 'ANDROID_SDK_ROOT="%s"\n' "$sdk_root"
} >> "$tmp_environment"
install -m 0644 "$tmp_environment" /etc/environment
rm -f "$tmp_environment"

mkdir -p /etc/profile.d /usr/local/bin
{
  printf 'export ANDROID_HOME=%s\n' "$sdk_root"
  printf 'export ANDROID_SDK_ROOT=%s\n' "$sdk_root"
  printf 'export PATH="%s/cmdline-tools/latest/bin:%s/platform-tools:%s/emulator:%s/build-tools/%s:$PATH"\n' "$sdk_root" "$sdk_root" "$sdk_root" "$sdk_root" "$build_tools_version"
} > /etc/profile.d/bastion-android-sdk.sh
chmod 0644 /etc/profile.d/bastion-android-sdk.sh

ln -sf "$sdkmanager" /usr/local/bin/sdkmanager
ln -sf "$avdmanager" /usr/local/bin/avdmanager
for tool in adb emulator; do
  if [ -x "$sdk_root/platform-tools/$tool" ]; then
    ln -sf "$sdk_root/platform-tools/$tool" "/usr/local/bin/$tool"
  elif [ -x "$sdk_root/emulator/$tool" ]; then
    ln -sf "$sdk_root/emulator/$tool" "/usr/local/bin/$tool"
  fi
done
for tool in aapt aapt2 aidl apksigner d8 zipalign; do
  if [ -x "$sdk_root/build-tools/$build_tools_version/$tool" ]; then
    ln -sf "$sdk_root/build-tools/$build_tools_version/$tool" "/usr/local/bin/$tool"
  fi
done

ANDROID_HOME="$sdk_root" ANDROID_SDK_ROOT="$sdk_root" "$sdkmanager" --sdk_root="$sdk_root" --version
ANDROID_HOME="$sdk_root" ANDROID_SDK_ROOT="$sdk_root" "$avdmanager" list target >/dev/null
ANDROID_HOME="$sdk_root" ANDROID_SDK_ROOT="$sdk_root" "$sdk_root/platform-tools/adb" version
ANDROID_HOME="$sdk_root" ANDROID_SDK_ROOT="$sdk_root" "$sdk_root/emulator/emulator" -version
test -d "$sdk_root/platforms/android-$api_level"
test -d "$sdk_root/build-tools/$build_tools_version"
