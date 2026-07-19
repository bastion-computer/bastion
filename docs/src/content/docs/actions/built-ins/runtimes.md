---
title: Runtimes
description: Built-in Bastion actions for runtime setup.
---

Runtime actions install language runtimes or runtime managers in guest VMs.

## `setup_node`

`setup_node` installs a Node.js major version from NodeSource.

| Input     | Required | Default | Description                       |
| --------- | -------- | ------- | --------------------------------- |
| `version` | Yes      | None    | Node.js major version to install. |

Example:

```json
{
  "agents": {
    "opencode": {}
  },
  "actions": {
    "init": [
      {
        "use": "setup_node",
        "with": {
          "version": 24
        }
      }
    ]
  }
}
```

The action installs the selected Node.js release with apt packages from
NodeSource.

## `setup_bun`

`setup_bun` installs Bun with the official installer.

| Input     | Required | Default | Description                                           |
| --------- | -------- | ------- | ----------------------------------------------------- |
| `version` | No       | Latest  | Bun release tag to install, for example `bun-v1.3.3`. |

Example:

```json
{
  "agents": {
    "opencode": {}
  },
  "actions": {
    "init": [
      {
        "use": "setup_bun",
        "with": {
          "version": "bun-v1.3.3"
        }
      }
    ]
  }
}
```

Omit `version` to install the latest Bun release:

```json
{
  "agents": {
    "opencode": {}
  },
  "actions": {
    "init": [{ "use": "setup_bun" }]
  }
}
```

The action installs Bun to `/usr/local/bin/bun` and verifies the installation
with `bun --version` and `bun --revision`.

## `setup_mise`

`setup_mise` installs mise and activates it for root shells by updating
`/root/.bashrc`.

| Input     | Required | Default | Description                                        |
| --------- | -------- | ------- | -------------------------------------------------- |
| `version` | No       | Latest  | mise version to install, for example `v2025.12.0`. |

Example:

```json
{
  "agents": {
    "opencode": {}
  },
  "actions": {
    "init": [
      {
        "use": "setup_mise",
        "with": {
          "version": "v2025.12.0"
        }
      }
    ]
  }
}
```

Omit `version` to install the latest mise release:

```json
{
  "agents": {
    "opencode": {}
  },
  "actions": {
    "init": [{ "use": "setup_mise" }]
  }
}
```

## `setup_uv`

`setup_uv` installs uv and uvx with the official standalone installer.

| Input            | Required | Default                | Description                                            |
| ---------------- | -------- | ---------------------- | ------------------------------------------------------ |
| `version`        | No       | Latest                 | uv version to install, for example `0.11.25`.          |
| `python_version` | No       | No Python installation | Python version to install with uv, for example `3.13`. |

Example:

```json
{
  "agents": {
    "opencode": {}
  },
  "actions": {
    "init": [
      {
        "use": "setup_uv",
        "with": {
          "version": "0.11.25",
          "python_version": "3.13"
        }
      },
      {
        "run": "uv sync",
        "working_directory": "/workspace/project"
      }
    ]
  }
}
```

Omit `version` to install the latest uv release, and omit `python_version` when a
template only needs uv without pre-installing a managed Python:

```json
{
  "agents": {
    "opencode": {}
  },
  "actions": {
    "init": [{ "use": "setup_uv" }]
  }
}
```

When `python_version` is provided, the action runs `uv python install` and
verifies the installed interpreter with `uv python find`.

## `setup_openjdk`

`setup_openjdk` installs an OpenJDK JDK from Ubuntu's apt repositories.

| Input     | Required | Default | Description                                       |
| --------- | -------- | ------- | ------------------------------------------------- |
| `version` | No       | Latest  | OpenJDK major version to install, for example 21. |

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
      }
    ]
  }
}
```

Omit `version` to install the latest OpenJDK JDK package available from the
guest apt repositories:

```json
{
  "agents": {
    "opencode": {}
  },
  "actions": {
    "init": [{ "use": "setup_openjdk" }]
  }
}
```

The action installs the selected `openjdk-<version>-jdk-headless` package, sets
`JAVA_HOME`, and verifies the installation with `java -version` and
`javac -version`.
