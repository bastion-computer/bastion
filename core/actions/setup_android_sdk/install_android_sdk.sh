#!/usr/bin/env sh
set -eu

api_level="${BASTION_INPUT_API_LEVEL:-}"
avd_device="${BASTION_INPUT_AVD_DEVICE:-}"
avd_name="${BASTION_INPUT_AVD_NAME:-}"
avd_system_image="${BASTION_INPUT_AVD_SYSTEM_IMAGE:-}"
build_tools_version="${BASTION_INPUT_BUILD_TOOLS_VERSION:-}"
create_avd="${BASTION_INPUT_CREATE_AVD:-false}"
extra_packages="${BASTION_INPUT_EXTRA_PACKAGES:-}"
sdk_root="/opt/android-sdk"
repository_url="https://dl.google.com/android/repository/repository2-1.xml"
repository_base_url="https://dl.google.com/android/repository"

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

if ! command -v java >/dev/null 2>&1; then
  printf 'java is required for Android SDK command-line tools; run setup_openjdk before setup_android_sdk\n' >&2
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

case "$create_avd" in
  true|false)
    ;;
  '')
    create_avd="false"
    ;;
  *)
    printf 'create_avd must be true or false: %s\n' "$create_avd" >&2
    exit 2
    ;;
esac

if [ -n "$avd_name" ]; then
  case "$avd_name" in
    *[!0-9A-Za-z._-]*)
      printf 'Android AVD name must contain only letters, numbers, dots, underscores, or dashes: %s\n' "$avd_name" >&2
      exit 2
      ;;
  esac
fi

reject_newline BASTION_INPUT_AVD_DEVICE "$avd_device"

if [ -n "$avd_system_image" ]; then
  reject_newline BASTION_INPUT_AVD_SYSTEM_IMAGE "$avd_system_image"

  case "$avd_system_image" in
    *' '*|*'	'*)
      printf 'Android AVD system image package must not contain whitespace: %s\n' "$avd_system_image" >&2
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

if [ "$create_avd" = "true" ]; then
  if [ -z "$avd_name" ]; then
    avd_name="pixel_9"
  fi

  if [ -z "$avd_device" ]; then
    avd_device="pixel_9"
  fi

  if [ -z "$avd_system_image" ]; then
    case "$(uname -m)" in
      x86_64|amd64)
        avd_system_image_abi="x86_64"
        ;;
      aarch64|arm64)
        avd_system_image_abi="arm64-v8a"
        ;;
      *)
        printf 'Unsupported Android emulator system image architecture: %s\n' "$(uname -m)" >&2
        exit 2
        ;;
    esac

    avd_system_image="system-images;android-$api_level;google_apis;$avd_system_image_abi"
  fi
fi

yes | "$sdkmanager" --sdk_root="$sdk_root" --licenses >/dev/null
yes | "$sdkmanager" --sdk_root="$sdk_root" \
  "platform-tools" \
  "emulator" \
  "platforms;android-$api_level" \
  "build-tools;$build_tools_version"

if [ "$create_avd" = "true" ]; then
  yes | "$sdkmanager" --sdk_root="$sdk_root" "$avd_system_image"
fi

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

if [ "$create_avd" = "true" ]; then
  mkdir -p /root/.android/avd
  printf 'no\n' | ANDROID_HOME="$sdk_root" ANDROID_SDK_ROOT="$sdk_root" "$avdmanager" create avd \
    --force \
    --name "$avd_name" \
    --package "$avd_system_image" \
    --device "$avd_device"
fi

ANDROID_HOME="$sdk_root" ANDROID_SDK_ROOT="$sdk_root" "$sdkmanager" --sdk_root="$sdk_root" --version
ANDROID_HOME="$sdk_root" ANDROID_SDK_ROOT="$sdk_root" "$avdmanager" list target >/dev/null
ANDROID_HOME="$sdk_root" ANDROID_SDK_ROOT="$sdk_root" "$sdk_root/platform-tools/adb" version
ANDROID_HOME="$sdk_root" ANDROID_SDK_ROOT="$sdk_root" "$sdk_root/emulator/emulator" -version
test -d "$sdk_root/platforms/android-$api_level"
test -d "$sdk_root/build-tools/$build_tools_version"
if [ "$create_avd" = "true" ]; then
  ANDROID_HOME="$sdk_root" ANDROID_SDK_ROOT="$sdk_root" "$avdmanager" list avd | grep -q "Name: $avd_name"
  test -f "/root/.android/avd/$avd_name.ini"
  test -d "/root/.android/avd/$avd_name.avd"
fi
