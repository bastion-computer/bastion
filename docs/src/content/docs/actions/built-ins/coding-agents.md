---
title: Coding Agents
description: Built-in Bastion actions for coding agents.
---

Coding agent actions install and configure agent CLIs inside the guest VM.

Host environment substitutions such as `${{ env.ANTHROPIC_API_KEY }}` are
resolved when the environment is created. The resolved values are then passed
into the guest action and may be written to guest files by the action. Use scoped
API keys appropriate for the environment.

## `setup_opencode`

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
