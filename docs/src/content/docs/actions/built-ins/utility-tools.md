---
title: Utility Tools
description: Built-in Bastion actions for development utilities.
---

Utility tool actions install CLIs and helper tools commonly needed by coding
agents in guest VMs.

## `set_default_ssh_directory`

`set_default_ssh_directory` configures interactive root SSH shells to start in a
specific directory when that directory exists.

| Input  | Required | Default | Description                                     |
| ------ | -------- | ------- | ----------------------------------------------- |
| `path` | Yes      | None    | Directory to use as the default SSH shell path. |

Example:

```json
{
  "agents": {
    "opencode": {}
  },
  "actions": {
    "init": [
      {
        "use": "set_default_ssh_directory",
        "with": {
          "path": "/workspace/bastion"
        }
      }
    ]
  }
}
```

The action stores the configured path under `/etc/bastion` and updates
`/root/.bashrc` with a guarded `cd`, so shells keep their normal start directory
if the configured path does not exist.

## `write_env_file`

`write_env_file` writes a `.env` file in a target directory inside the guest VM.
The target directory is created when needed.

| Input  | Required | Default | Description                                  |
| ------ | -------- | ------- | -------------------------------------------- |
| `path` | Yes      | None    | Directory where `.env` should be written to. |

The variables to write come from the action `context` object. Context keys must
be valid environment variable names. String, number, boolean, and `null` values
are converted to env values; arrays and objects are written as compact JSON
strings. The generated `.env` file uses shell-compatible quoting and mode `600`.

Example:

```json
{
  "agents": {
    "opencode": {}
  },
  "actions": {
    "init": [
      {
        "use": "write_env_file",
        "with": {
          "path": "/workspace/bastion"
        },
        "context": {
          "NODE_ENV": "development",
          "SOME_VAR_1": "${{ secret.SOME_VAR_1 }}",
          "SOME_VAR_2": "${{ secret.SOME_VAR_2 }}",
          "FEATURE_FLAGS": {
            "localDev": true
          }
        }
      }
    ]
  }
}
```

This writes `/workspace/bastion/.env` during template creation. Use it in
`actions.start` instead when the file should be regenerated for every new
environment.

## `setup_github_cli`

`setup_github_cli` installs `gh` from GitHub's apt repository and configures it
and `git` for non-interactive use in the guest.

| Input          | Required | Default                  | Description                                                 |
| -------------- | -------- | ------------------------ | ----------------------------------------------------------- |
| `token`        | Yes      | None                     | GitHub personal access token used by `gh` inside the guest. |
| `hostname`     | No       | `github.com`             | GitHub hostname to target.                                  |
| `git_protocol` | No       | `https`                  | Git protocol for `gh` operations. Must be `https` or `ssh`. |
| `name`         | No       | `bastion-agent`          | Global `git` `user.name` value.                             |
| `email`        | No       | `agent@bastion.computer` | Global `git` `user.email` value.                            |

Example:

```json
{
  "agents": {
    "opencode": {}
  },
  "actions": {
    "init": [
      {
        "use": "setup_github_cli",
        "with": {
          "token": "${{ secret.GITHUB_TOKEN }}",
          "hostname": "github.com",
          "git_protocol": "https"
        }
      }
    ]
  }
}
```

The action also runs `gh auth setup-git` and configures `git` to use the
GitHub CLI credential helper. Global `git` identity defaults to
`bastion-agent <agent@bastion.computer>` and can be overridden with `name`
and `email`.

The token is stored in the guest at `/etc/bastion/github-token` with mode `600`.
Treat environments created with this action as having access to that token.

## `setup_aws_cli`

`setup_aws_cli` installs AWS CLI v2 with AWS's official Linux command line
installer and configures a profile for IAM access key authentication.

| Input               | Required | Default   | Description                                                 |
| ------------------- | -------- | --------- | ----------------------------------------------------------- |
| `access_key_id`     | Yes      | None      | AWS access key ID used by the AWS CLI inside the guest.     |
| `secret_access_key` | Yes      | None      | AWS secret access key used by the AWS CLI inside the guest. |
| `region`            | Yes      | None      | Default AWS Region for the configured profile.              |
| `session_token`     | No       | None      | AWS session token for temporary IAM credentials.            |
| `profile`           | No       | `default` | AWS CLI profile name to configure.                          |
| `output`            | No       | `json`    | Default AWS CLI output format: `json`, `yaml`, `text`, etc. |

Example:

```json
{
  "agents": {
    "opencode": {}
  },
  "actions": {
    "init": [
      {
        "use": "setup_aws_cli",
        "with": {
          "access_key_id": "${{ secret.AWS_ACCESS_KEY_ID }}",
          "secret_access_key": "${{ secret.AWS_SECRET_ACCESS_KEY }}",
          "region": "us-west-2",
          "profile": "default",
          "output": "json"
        }
      }
    ]
  }
}
```

For temporary IAM credentials, also pass `session_token`:

```json
{
  "use": "setup_aws_cli",
  "with": {
    "access_key_id": "${{ secret.AWS_ACCESS_KEY_ID }}",
    "secret_access_key": "${{ secret.AWS_SECRET_ACCESS_KEY }}",
    "session_token": "${{ secret.AWS_SESSION_TOKEN }}",
    "region": "us-east-1"
  }
}
```

The action downloads the official AWS CLI v2 Linux installer for the guest CPU
architecture.

Credentials are written to root's AWS shared config files under `/root/.aws` with
mode `600`, matching the AWS CLI's standard configuration file behavior. Treat
templates and environments created with this action as having access to the
configured IAM credentials.

## `setup_docker`

`setup_docker` installs Docker Engine from Docker's official Ubuntu apt
repository and starts the `docker` service.

This action does not accept inputs.

Example:

```json
{
  "agents": {
    "opencode": {}
  },
  "actions": {
    "init": [
      {
        "use": "setup_docker"
      }
    ]
  }
}
```

The action removes conflicting distro Docker packages when present, configures
Docker's apt repository, installs `docker-ce`, `docker-ce-cli`, `containerd.io`,
`docker-buildx-plugin`, and `docker-compose-plugin`, then verifies that the
daemon is reachable with `docker info`.

## `setup_android_sdk`

`setup_android_sdk` installs and configures the Android SDK command-line
toolchain required for Android app builds and emulator management.

This action requires Java to already be installed. Use `setup_openjdk` earlier in
the same template when the base image does not already include a JDK.

| Input                 | Required | Default       | Description                                                        |
| --------------------- | -------- | ------------- | ------------------------------------------------------------------ |
| `api_level`           | No       | Latest stable | Android API level to install, for example 36.                      |
| `avd_device`          | No       | `pixel_9`     | Android device profile to use when `create_avd` is `true`.         |
| `avd_name`            | No       | `pixel_9`     | Android Virtual Device name to create when `create_avd` is `true`. |
| `avd_system_image`    | No       | Host ABI      | Android SDK system image package to install for the AVD.           |
| `build_tools_version` | No       | Latest stable | Android SDK Build Tools version to install, for example `36.0.0`.  |
| `create_avd`          | No       | `false`       | Create an Android Virtual Device after installing the SDK.         |
| `extra_packages`      | No       | None          | Whitespace-separated additional `sdkmanager` packages to install.  |

Example:

```json
{
  "agents": {
    "opencode": {}
  },
  "actions": {
    "init": [
      {
        "use": "setup_openjdk",
        "with": {
          "version": 21
        }
      },
      {
        "use": "setup_android_sdk",
        "with": {
          "api_level": 36,
          "build_tools_version": "36.0.0"
        }
      }
    ]
  }
}
```

To preinstall emulator system images or other Android SDK packages, pass their
SDK package paths with `extra_packages`:

```json
{
  "use": "setup_android_sdk",
  "with": {
    "api_level": 36,
    "extra_packages": "system-images;android-36;google_apis;x86_64"
  }
}
```

To create an Android Virtual Device, set `create_avd` to `true`. By default,
the action creates a Pixel 9 AVD named `pixel_9` and installs
`system-images;android-<api_level>;google_apis;<host ABI>`:

```json
{
  "use": "setup_android_sdk",
  "with": {
    "api_level": 36,
    "create_avd": true
  }
}
```

The action exposes common tools such as `sdkmanager`, `avdmanager`, `adb`, and `emulator`
on `PATH`. When `create_avd` is `true`, it also installs the selected system image
and creates the AVD under `/root/.android/avd`.

## `setup_maestro`

`setup_maestro` installs the Maestro CLI for mobile end-to-end testing.

This action requires Java 17 or newer to already be installed. Use
`setup_openjdk` earlier in the same template when the base image does not already
include a compatible JDK.

For Android emulator tests, use `setup_android_sdk` earlier in the same template
so Android tools such as `adb` are available.

| Input     | Required | Default | Description                                           |
| --------- | -------- | ------- | ----------------------------------------------------- |
| `version` | No       | Latest  | Maestro CLI version to install, for example `1.39.0`. |

Example:

```json
{
  "agents": {
    "opencode": {}
  },
  "actions": {
    "init": [
      {
        "use": "setup_openjdk",
        "with": {
          "version": 21
        }
      },
      {
        "use": "setup_android_sdk",
        "with": {
          "api_level": 36,
          "build_tools_version": "36.0.0"
        }
      },
      {
        "use": "setup_maestro",
        "with": {
          "version": "1.39.0"
        }
      }
    ]
  }
}
```

Omit `version` to install the latest Maestro CLI release:

```json
{
  "agents": {
    "opencode": {}
  },
  "actions": {
    "init": [
      { "use": "setup_openjdk" },
      { "use": "setup_android_sdk" },
      { "use": "setup_maestro" }
    ]
  }
}
```

The action uses Maestro's official installer with `MAESTRO_VERSION` when a
version is provided and verifies the installation with `maestro --version`
and `maestro --help`.
