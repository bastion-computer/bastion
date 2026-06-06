---
title: Custom Actions
description: Create reusable Bastion template actions.
---

Custom actions are reusable packages referenced by `use` actions in templates.
They live under `<data-dir>/actions` on the host and are copied into the guest
when a template is created.

Use a custom action when a setup step should be packaged once and shared across
templates instead of repeated as an inline `run` command.

Built-in actions are documented separately by category:

| Category                                           | Actions                                         |
| -------------------------------------------------- | ----------------------------------------------- |
| [Coding agents](/actions/built-ins/coding-agents/) | `setup_opencode`                                |
| [Utility tools](/actions/built-ins/utility-tools/) | `set_default_ssh_directory`, `setup_github_cli` |
| [Runtimes](/actions/built-ins/runtimes/)           | `setup_node`, `setup_mise`                      |

## Package Layout

A custom action is a directory with a `manifest.json` file and any scripts or
files needed by the action.

```text
~/.bastion/actions/setup_python/
├── manifest.json
└── install.sh
```

Example manifest:

```json title="manifest.json"
{
  "inputs": {
    "version": {
      "type": "string",
      "description": "Python version to install.",
      "required": true
    }
  },
  "run": "sh ./install.sh"
}
```

Example script:

```sh title="install.sh"
#!/usr/bin/env sh
set -eu

printf 'Installing Python %s\n' "$BASTION_INPUT_VERSION"
```

## Manifest Fields

| Field    | Required | Description                                                          |
| -------- | -------- | -------------------------------------------------------------------- |
| `run`    | Yes      | Command executed from the action directory inside the guest.         |
| `inputs` | No       | Input definitions accepted from the template action's `with` object. |

Input definitions support these fields:

| Field         | Required | Description                                              |
| ------------- | -------- | -------------------------------------------------------- |
| `type`        | Yes      | One of `string`, `number`, or `boolean`.                 |
| `description` | No       | Human-readable input description.                        |
| `required`    | No       | Whether the input must be provided. Defaults to `false`. |

Unknown inputs, missing required inputs, and type mismatches fail environment
creation.

## Input Environment Variables

Values from `with` are exposed to the action command as environment variables
with the `BASTION_INPUT_` prefix.

| Input          | Environment variable         |
| -------------- | ---------------------------- |
| `version`      | `BASTION_INPUT_VERSION`      |
| `node_version` | `BASTION_INPUT_NODE_VERSION` |

Values are stringified before they are written into the guest-side action input
environment file.

## Use a Custom Action

After adding a directory under `<data-dir>/actions`, reference the directory name
from a template:

```json
{
  "actions": {
    "init": [
      {
        "use": "setup_python",
        "with": {
          "version": "3.12"
        }
      }
    ]
  }
}
```

Action names must start with a letter and can contain letters, numbers,
underscores, and hyphens.

## Seeding and Overrides

`bastiond` seeds built-in action packages into `<data-dir>/actions` when it
starts. Seeding overwrites directories whose names match built-in actions so
updated built-in action packages take effect after a Bastion upgrade.

Use a unique action name for custom actions. Directories with built-in action
names are managed by Bastion and will be replaced on `bastiond` startup.
