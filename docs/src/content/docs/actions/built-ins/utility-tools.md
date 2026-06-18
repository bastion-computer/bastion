---
title: Utility Tools
description: Built-in Bastion actions for development utilities.
---

Utility tool actions install CLIs and helper tools commonly needed by coding
agents in guest VMs.

Secret references such as `${{ secret.GITHUB_TOKEN }}` are resolved before
guest actions run. The resolved values are passed into the guest action and may
be written to guest files by the action. Use scoped tokens appropriate for the
environment.

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
