---
title: Built-in actions
description: Inputs, defaults, prerequisites, installed paths, and behavior for every built-in Bastion action.
---

Bastion embeds 12 action packages. `bastion start daemon` installs them under
`<DATA_DIR>/actions` and replaces those package directories on every daemon
start.

All built-in actions run as `root` in the Ubuntu guest. Most download packages
or installers from the public internet and therefore require outbound network
and DNS access. Their command output appears in the template or environment
operation stream.

Bastion supports only x86_64 guests. Some embedded action scripts contain
aarch64 or arm64 selection branches, but those branches do not represent a
supported guest platform and are not covered by Bastion's supported host path.

Use an action in either `actions.init` or `actions.start`:

```json
{
  "use": "setup_node",
  "with": {
    "version": 24
  }
}
```

`actions.init` changes become part of the immutable template overlay.
`actions.start` runs independently for every environment. See the
[template lifecycle reference](/reference/template-configuration/#lifecycle-action-configuration).

> **Warning:** Credential actions store resolved credentials in guest files.
> Secret values used during init can also persist in template archives. Use
> narrowly scoped credentials and treat resulting templates and environments as
> sensitive. See
> [Security and operational limits](/explanation/security-and-operational-limits/).

> **Warning:** Version inputs do not pin every downloaded artifact.
>
> Package repositories can select newer transitive packages, major-only inputs
> can select newer patch releases, and `Latest` defaults and remote installer
> scripts can change over time. The actions do not provide a complete
> reproducible-build or supply-chain-verification boundary. For reproducible or
> high-assurance templates, mirror and verify artifacts, pin repository state,
> or use a reviewed custom action.

## `set_default_ssh_directory` action

Configures interactive root SSH shells to change to a directory when it exists.

| Input  | Type   | Required | Default | Description                                 |
| ------ | ------ | -------- | ------- | ------------------------------------------- |
| `path` | string | Yes      | None    | Guest directory for interactive SSH shells. |

The action rejects newlines in `path`, writes the value to
`/etc/bastion/default-ssh-directory` with mode `0600`, and installs a guarded
block in `/root/.bashrc`. The shell keeps its normal directory if the configured
path does not exist. Running the action again replaces its previous Bash block.

```json
{
  "use": "set_default_ssh_directory",
  "with": {
    "path": "/workspace/project"
  }
}
```

## `setup_android_sdk` action

Installs Android command-line tools, platform tools, the emulator, one Android
platform, and one Build Tools version under `/opt/android-sdk`.

| Input                 | Type    | Required | Default                                                     | Description                                                    |
| --------------------- | ------- | -------- | ----------------------------------------------------------- | -------------------------------------------------------------- |
| `api_level`           | number  | No       | Latest platform reported by `sdkmanager`                    | Android API level, such as `36`; it must represent an integer. |
| `avd_device`          | string  | No       | `pixel_9`                                                   | Device profile used when `create_avd` is `true`.               |
| `avd_name`            | string  | No       | `pixel_9`                                                   | AVD name used when `create_avd` is `true`.                     |
| `avd_system_image`    | string  | No       | `system-images;android-<API_LEVEL>;google_apis;<GUEST_ABI>` | Full SDK package for the AVD system image.                     |
| `build_tools_version` | string  | No       | Latest version reported by `sdkmanager`                     | Build Tools version, such as `36.0.0`.                         |
| `create_avd`          | boolean | No       | `false`                                                     | Install a system image and create an AVD.                      |
| `extra_packages`      | string  | No       | Empty                                                       | Whitespace-separated additional `sdkmanager` package names.    |

`API_LEVEL` and `GUEST_ABI` in the default description are placeholders. The
supported x86_64 guest path uses `x86_64`. The script's `arm64-v8a` branch is
unsupported.

Java must already be available. Place `setup_openjdk` before this action when
needed. The action accepts SDK licenses noninteractively and installs Ubuntu
packages required by Android tools and the emulator.

It writes `ANDROID_HOME` and `ANDROID_SDK_ROOT` to `/etc/environment`, installs
a profile script, creates `/root/Android/sdk` as a symlink to `/opt/android-sdk`,
and links common tools such as `sdkmanager`, `avdmanager`, `adb`, `emulator`,
`aapt2`, and `apksigner` into `/usr/local/bin` when present.

When `create_avd` is `true`, the AVD is stored under `/root/.android/avd`.
Creating an AVD does not by itself guarantee that the VM host exposes the nested
virtualization features required to run an emulator.

## `setup_aws_cli` action

Installs AWS CLI v2 with the official Linux installer and writes one profile for
root.

| Input               | Type   | Required | Default   | Description                                               |
| ------------------- | ------ | -------- | --------- | --------------------------------------------------------- |
| `access_key_id`     | string | Yes      | None      | IAM access key ID.                                        |
| `secret_access_key` | string | Yes      | None      | IAM secret access key.                                    |
| `region`            | string | Yes      | None      | Default AWS Region.                                       |
| `session_token`     | string | No       | Empty     | Session token for temporary credentials.                  |
| `profile`           | string | No       | `default` | AWS shared-config profile name.                           |
| `output`            | string | No       | `json`    | `json`, `yaml`, `yaml-stream`, `text`, `table`, or `off`. |

The supported guest path uses the official x86_64 installer. The script also
selects the official aarch64 installer on that architecture, but Bastion does
not support arm64 guests. It installs AWS CLI under `/usr/local/aws-cli` and
exposes `/usr/local/bin/aws`.

Credentials and configuration are written under `/root/.aws`. The directory has
mode `0700`; existing `config` and `credentials` files are set to mode `0600`.
Values cannot contain newlines.

```json
{
  "use": "setup_aws_cli",
  "with": {
    "access_key_id": "${{ secret.AWS_ACCESS_KEY_ID }}",
    "secret_access_key": "${{ secret.AWS_SECRET_ACCESS_KEY }}",
    "session_token": "${{ secret.AWS_SESSION_TOKEN }}",
    "region": "us-east-1",
    "profile": "default"
  }
}
```

## `setup_bun` action

Installs Bun with the official installer.

| Input     | Type   | Required | Default | Description                                       |
| --------- | ------ | -------- | ------- | ------------------------------------------------- |
| `version` | string | No       | Latest  | Installer release argument, such as `bun-v1.3.3`. |

The action installs `bash`, `ca-certificates`, `curl`, and `unzip`, sets
`BUN_INSTALL=/usr/local`, and verifies `/usr/local/bin/bun` with `--version` and
`--revision`.

## `setup_docker` action

Installs Docker Engine from Docker's official Ubuntu apt repository. This action
accepts no inputs.

It removes installed conflicting packages from this set when present:
`docker.io`, `docker-compose`, `docker-compose-v2`, `docker-doc`,
`podman-docker`, `containerd`, and `runc`. It then installs `docker-ce`,
`docker-ce-cli`, `containerd.io`, `docker-buildx-plugin`, and
`docker-compose-plugin`.

The action enables and starts `docker.service`, retries `docker info` up to 30
times with a one-second sleep after each failed attempt, and then runs a final
`docker info` verification after checking Docker, Buildx, and Compose versions.
This is attempt-based retry behavior, not a hard 30-second elapsed deadline. The
guest must use systemd and identify an Ubuntu codename for the apt repository.

## `setup_github_cli` action

Installs GitHub CLI from GitHub's apt repository and configures `gh` and Git for
noninteractive root-user access.

| Input          | Type   | Required | Default                  | Description                           |
| -------------- | ------ | -------- | ------------------------ | ------------------------------------- |
| `token`        | string | Yes      | None                     | GitHub personal access token.         |
| `hostname`     | string | No       | `github.com`             | GitHub or GitHub Enterprise hostname. |
| `git_protocol` | string | No       | `https`                  | `https` or `ssh`.                     |
| `name`         | string | No       | `bastion-agent`          | Global Git `user.name`.               |
| `email`        | string | No       | `agent@bastion.computer` | Global Git `user.email`.              |

`hostname` can contain only letters, numbers, dots, and hyphens.

The action installs `/usr/bin/gh` and places a wrapper at `/usr/local/bin/gh`.
The wrapper reads `/etc/bastion/github-cli.env` and
`/etc/bastion/github-token`, exports `GH_TOKEN`, `GH_ENTERPRISE_TOKEN`, and
`GH_HOST`, disables interactive prompts and update notices, then invokes the
packaged CLI.

Both `/etc/bastion` files use mode `0600`. The action runs
`gh auth setup-git`, configures the GitHub CLI credential helper for the target
host, and sets global root-user Git identity.

```json
{
  "use": "setup_github_cli",
  "with": {
    "token": "${{ secret.GITHUB_TOKEN }}",
    "hostname": "github.com",
    "git_protocol": "https"
  }
}
```

## `setup_maestro` action

Installs the Maestro mobile testing CLI with its official installer.

| Input     | Type   | Required | Default | Description                                |
| --------- | ------ | -------- | ------- | ------------------------------------------ |
| `version` | string | No       | Latest  | Maestro release version, such as `1.39.0`. |

Java 17 or newer must already be available. The action derives `JAVA_HOME` when
it is absent, installs Maestro under `/usr/local/maestro`, links
`/usr/local/bin/maestro`, and writes `MAESTRO_DIR` to `/etc/environment` and a
profile script.

For Android tests, place `setup_android_sdk` before this action so `adb` and
other Android tools are present. Maestro itself does not require the Android SDK
for installation.

## `setup_mise` action

Installs mise with the official installer.

| Input     | Type   | Required | Default | Description                         |
| --------- | ------ | -------- | ------- | ----------------------------------- |
| `version` | string | No       | Latest  | mise version, such as `v2025.12.0`. |

The action sets `MISE_INSTALL_PATH=/usr/local/bin/mise`. When `version` is
present, it sets `MISE_VERSION` for the installer. It also adds
`eval "$(/usr/local/bin/mise activate bash)"` to `/root/.bashrc` if that exact
line is not already present.

## `setup_node` action

Installs Node.js from NodeSource's Ubuntu-compatible apt repository.

| Input     | Type   | Required | Default | Description                          |
| --------- | ------ | -------- | ------- | ------------------------------------ |
| `version` | number | Yes      | None    | Node.js major version, such as `24`. |

The numeric value must begin with a valid integer major. Bastion configures the
`node_<MAJOR>.x` NodeSource repository, installs the `nodejs` package, and runs
`node --version`. `MAJOR` represents the selected major version.

## `setup_openjdk` action

Installs a headless OpenJDK JDK from the guest's apt repositories.

| Input     | Type   | Required | Default                                                  | Description                  |
| --------- | ------ | -------- | -------------------------------------------------------- | ---------------------------- |
| `version` | number | No       | Highest available `openjdk-<MAJOR>-jdk-headless` package | OpenJDK major, such as `21`. |

The action installs `openjdk-<MAJOR>-jdk-headless`, derives `JAVA_HOME` from
`javac`, writes it to `/etc/environment`, installs a profile script, and verifies
`java` and `javac`.

## `setup_uv` action

Installs uv and uvx with Astral's standalone installer.

| Input            | Type   | Required | Default                | Description                                                   |
| ---------------- | ------ | -------- | ---------------------- | ------------------------------------------------------------- |
| `version`        | string | No       | Latest                 | uv version, such as `0.11.25`.                                |
| `python_version` | string | No       | No Python installation | Python version passed to `uv python install`, such as `3.13`. |

The action sets `UV_UNMANAGED_INSTALL=/usr/local/bin`. A specified uv version is
inserted into the installer URL. When `python_version` is present, the action
runs `uv python install` and verifies it with `uv python find`.

## `write_env_file` action

Writes a sorted, shell-compatible `.env` file from the invocation's `context`.

| Input  | Type   | Required | Default | Description                               |
| ------ | ------ | -------- | ------- | ----------------------------------------- |
| `path` | string | Yes      | None    | Guest directory in which to write `.env`. |

`context` is also required by this action and must be a JSON object:

```json
{
  "use": "write_env_file",
  "with": {
    "path": "/workspace/project"
  },
  "context": {
    "NODE_ENV": "development",
    "API_TOKEN": "${{ secret.API_TOKEN }}",
    "RETRY_COUNT": 3,
    "FEATURE_ENABLED": true,
    "OPTIONAL_VALUE": null,
    "FEATURE_FLAGS": {
      "localDev": true
    }
  }
}
```

Context keys must match `^[A-Za-z_][A-Za-z0-9_]*$`. Values convert as follows:

| JSON value        | `.env` value                                  |
| ----------------- | --------------------------------------------- |
| String            | Original string with shell-compatible quoting |
| Number or boolean | JSON scalar text                              |
| `null`            | Empty string                                  |
| Array or object   | Compact JSON text                             |

The action creates `path`, writes `<PATH>/.env` with mode `0600`, and replaces an
existing file. `PATH` represents the supplied guest directory. The base includes
`jq`, which this action uses for validation and conversion.
