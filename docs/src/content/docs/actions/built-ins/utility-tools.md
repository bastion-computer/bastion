---
title: Utility Tools
description: Built-in Bastion actions for development utilities.
---

Utility tool actions install CLIs and helper tools commonly needed by coding
agents in guest VMs.

Host environment substitutions such as `${{ env.GITHUB_TOKEN }}` are resolved
when the template is created. The resolved values are then passed into the
guest action and may be written to guest files by the action. Use scoped tokens
appropriate for the environment.

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

The action also runs `gh auth setup-git` and configures `git` to use the
GitHub CLI credential helper for the target host. Global `git` identity defaults
to `bastion-agent <agent@bastion.computer>` and can be overridden with `name`
and `email`.

The token is stored in the guest at `/etc/bastion/github-token` with mode `600`.
Treat environments created with this action as having access to that token.

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
