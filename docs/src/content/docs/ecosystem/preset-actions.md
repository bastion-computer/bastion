---
title: Preset Actions
description: Use and author reusable Bastion template actions.
---

Preset actions are reusable packages referenced by `use` actions in templates.
They live under `<data-dir>/actions` on the host and are copied into the guest
when an environment is created.

`bastiond` seeds built-in actions into the data directory when it starts.

## Built-ins

| Action             | Inputs                                                                                                                                             | Description                                                              |
| ------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------ |
| `setup_node`       | `version` number, required                                                                                                                         | Installs a Node.js major version from NodeSource.                        |
| `setup_mise`       | `version` string, optional                                                                                                                         | Installs mise to `/usr/local/bin/mise` and activates it for root shells. |
| `setup_github_cli` | `token` string, required. `hostname` string and `git_protocol` string, optional.                                                                   | Installs and authenticates the GitHub CLI.                               |
| `setup_opencode`   | `provider`, `model`, and `api_key` strings, required. `small_model`, `base_url`, `share`, `permission`, `version`, and `config` strings, optional. | Installs OpenCode and writes its guest configuration.                    |

Example template:

```json
{
  "actions": {
    "init": [
      { "use": "setup_mise" },
      { "use": "setup_node", "with": { "version": 24 } },
      {
        "use": "setup_github_cli",
        "with": { "token": "${{ env.GITHUB_TOKEN }}" }
      },
      {
        "use": "setup_opencode",
        "with": {
          "provider": "anthropic",
          "model": "anthropic/claude-sonnet-4-20250514",
          "api_key": "${{ env.ANTHROPIC_API_KEY }}"
        }
      }
    ]
  }
}
```

Host environment substitutions such as `${{ env.GITHUB_TOKEN }}` are resolved
when the environment is created. The resolved values are then passed into the
guest action and may be written to guest files by the action. Use scoped tokens
and API keys appropriate for the environment.

## GitHub CLI Action

`setup_github_cli` installs `gh` from GitHub's apt repository and configures it
for non-interactive use in the guest.

| Input          | Required | Default      | Description                                                 |
| -------------- | -------- | ------------ | ----------------------------------------------------------- |
| `token`        | Yes      | None         | GitHub personal access token used by `gh` inside the guest. |
| `hostname`     | No       | `github.com` | GitHub hostname to target.                                  |
| `git_protocol` | No       | `https`      | Git protocol for `gh` operations. Must be `https` or `ssh`. |

Example:

```json
{
  "actions": {
    "init": [
      {
        "use": "setup_github_cli",
        "with": {
          "token": "${{ env.GITHUB_TOKEN }}",
          "hostname": "github.com",
          "git_protocol": "https"
        }
      }
    ]
  }
}
```

The action stores GitHub configuration under `/etc/bastion` and installs a
wrapper at `/usr/local/bin/gh`. The wrapper exports `GH_TOKEN`,
`GH_ENTERPRISE_TOKEN`, `GH_HOST`, and non-interactive update/prompt settings
before delegating to `/usr/bin/gh`.

The token is stored in the guest at `/etc/bastion/github-token` with mode `600`.
Treat environments created with this action as having access to that token.

## OpenCode Action

`setup_opencode` installs OpenCode with the official installer, links it to
`/usr/local/bin/opencode`, writes `/root/.config/opencode/opencode.json`, and
writes provider credentials to `/root/.local/share/opencode/auth.json`.

| Input         | Required | Default | Description                                                          |
| ------------- | -------- | ------- | -------------------------------------------------------------------- |
| `provider`    | Yes      | None    | OpenCode provider ID, for example `anthropic` or `openai`.           |
| `model`       | Yes      | None    | OpenCode model ID in `provider/model` format.                        |
| `api_key`     | Yes      | None    | Provider API key written to OpenCode auth config.                    |
| `small_model` | No       | None    | Optional small model ID for lightweight OpenCode tasks.              |
| `base_url`    | No       | None    | Optional provider `baseURL` override.                                |
| `share`       | No       | None    | OpenCode sharing mode. Must be `manual`, `auto`, or `disabled`.      |
| `permission`  | No       | None    | Default OpenCode permission mode. Must be `ask`, `allow`, or `deny`. |
| `version`     | No       | Latest  | OpenCode version to install, for example `1.0.180`.                  |
| `config`      | No       | None    | JSON object merged over the generated OpenCode config.               |

Example:

```json
{
  "actions": {
    "init": [
      {
        "use": "setup_opencode",
        "with": {
          "provider": "anthropic",
          "model": "anthropic/claude-sonnet-4-20250514",
          "api_key": "${{ env.ANTHROPIC_API_KEY }}",
          "permission": "ask",
          "share": "disabled"
        }
      }
    ]
  }
}
```

Use `config` when you need to set additional OpenCode configuration fields that
Bastion does not model directly:

```json
{
  "actions": {
    "init": [
      {
        "use": "setup_opencode",
        "with": {
          "provider": "openai",
          "model": "openai/gpt-5",
          "api_key": "${{ env.OPENAI_API_KEY }}",
          "config": "{\"theme\":\"system\"}"
        }
      }
    ]
  }
}
```

The `config` value must be a JSON object string. It is merged over the generated
OpenCode config, so fields in `config` can override generated values.

The provider API key is stored in the guest OpenCode auth file with mode `600`.
Treat environments created with this action as having access to that provider
credential.

## Package Layout

A preset action is a directory with a `manifest.json` file and any scripts or
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
