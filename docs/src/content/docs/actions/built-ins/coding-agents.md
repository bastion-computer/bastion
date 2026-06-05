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

`setup_opencode` installs OpenCode with the official installer and links it to
`/usr/local/bin/opencode`. When provided, it writes raw JSON object strings to
`/root/.config/opencode/opencode.json`, `/root/.config/opencode/tui.json`, and
`/root/.local/share/opencode/auth.json`.

| Input    | Required | Default                                                 | Description                                             |
| -------- | -------- | ------------------------------------------------------- | ------------------------------------------------------- |
| `auth`   | No       | Absent                                                  | JSON object string written to OpenCode `auth.json`.     |
| `config` | No       | Absent                                                  | JSON object string written to OpenCode `opencode.json`. |
| `tui`    | No       | `{"mouse": false, "keybinds": {"input_paste": "none"}}` | JSON object string written to OpenCode `tui.json`.      |

Example:

```json
{
  "actions": {
    "init": [
      {
        "use": "setup_opencode",
        "with": {
          "auth": "{\"anthropic\":{\"type\":\"api\",\"key\":\"${{ env.ANTHROPIC_API_KEY }}\"}}",
          "config": "{\"model\":\"anthropic/claude-sonnet-4-20250514\",\"permission\":\"ask\",\"share\":\"disabled\"}",
          "tui": "{\"mouse\":false,\"keybinds\":{\"input_paste\":\"none\"}}"
        }
      }
    ]
  }
}
```

Use `config` to pass the exact OpenCode configuration JSON you want in the guest:

```json
{
  "actions": {
    "init": [
      {
        "use": "setup_opencode",
        "with": {
          "auth": "{\"openai\":{\"type\":\"api\",\"key\":\"${{ env.OPENAI_API_KEY }}\"}}",
          "config": "{\"model\":\"openai/gpt-5\",\"theme\":\"system\"}"
        }
      }
    ]
  }
}
```

The `auth` and `config` values must be JSON object strings. Omitted values do
not create the corresponding OpenCode file. The `tui` value must also be a JSON
object string; when omitted, Bastion writes
`{"mouse": false, "keybinds": {"input_paste": "none"}}` so native terminal
selection works when OpenCode runs inside `bastion mux`. It also disables
OpenCode's `ctrl+v` clipboard reader, which reads the guest clipboard instead
of the local terminal clipboard over SSH.

When `auth` is provided, the credential is stored in the guest OpenCode auth
file with mode `600`. Treat environments created with this action as having
access to that credential.
