---
title: Utility Tools
description: Built-in Bastion actions for development utilities.
---

Utility tool actions install CLIs and helper tools commonly needed by coding
agents in guest VMs.

Host environment substitutions such as `${{ env.GITHUB_TOKEN }}` are resolved
when the environment is created. The resolved values are then passed into the
guest action and may be written to guest files by the action. Use scoped tokens
appropriate for the environment.

## `setup_github_cli`

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
