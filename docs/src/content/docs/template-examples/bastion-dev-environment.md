---
title: Bastion Dev Environment
description: An example of how we use Bastion to build Bastion.
---

This example creates a development environment for working on Bastion itself. It
installs mise and Docker, configures GitHub CLI, configures OpenCode, clones the
Bastion repository, installs the repository tools, exposes the docs dev server,
pulls the latest changes when each environment starts, and opens interactive SSH
shells in the repository directory.

## Template

Create `template.json`:

```json title="template.json"
{
  "resources": {
    "vcpu": 4,
    "memory": 8,
    "volume": 40
  },
  "agents": {
    "opencode": {
      "working_directory": "/workspace/bastion",
      "auth": {
        "openai": {
          "type": "api",
          "key": "${{ secret.OPENAI_API_KEY }}"
        }
      },
      "config": {
        "model": "openai/gpt-5.5",
        "permission": "allow",
        "agent": {
          "build": {
            "model": "openai/gpt-5.5",
            "variant": "xhigh"
          },
          "plan": {
            "model": "openai/gpt-5.5",
            "variant": "xhigh"
          }
        }
      }
    }
  },
  "tunnels": {
    "docs": 4321
  },
  "actions": {
    "init": [
      {
        "use": "setup_mise"
      },
      {
        "use": "setup_docker"
      },
      {
        "use": "setup_github_cli",
        "with": {
          "token": "${{ secret.GITHUB_TOKEN }}",
          "hostname": "github.com",
          "git_protocol": "https"
        }
      },
      {
        "run": "gh repo clone bastion-computer/bastion bastion",
        "working_directory": "/workspace"
      },
      {
        "run": "mise trust ./mise.toml && mise install",
        "working_directory": "/workspace/bastion"
      },
      {
        "use": "set_default_ssh_directory",
        "with": {
          "path": "/workspace/bastion"
        }
      }
    ],
    "start": [
      {
        "run": "git pull",
        "working_directory": "/workspace/bastion"
      }
    ]
  }
}
```

## Create the Environment

Complete [system setup](/guides/system/) and, if one does not already exist,
build the shared base before using this template:

```sh
bastion base build
```

Create the secrets referenced by the template before registering it:

```sh
bastion secrets create --key GITHUB_TOKEN --value ghp_xxxxxx
bastion secrets create --key OPENAI_API_KEY --value sk_xxxxxx
```

Register the template:

```sh
bastion templates create --key bastion-dev --file ./template.json
```

Launch an environment:

```sh
bastion env create --template-key bastion-dev --key bastion-dev-1 --tag dev
```

Open an interactive shell:

```sh
bastion ssh --key bastion-dev-1
```

The shell starts in `/workspace/bastion` because the template configures that path
as the default SSH directory.

## Change OpenCode Providers

The template uses OpenAI by default. To use Anthropic instead, create an
`ANTHROPIC_API_KEY` secret and change the `agents.opencode.auth` and
`agents.opencode.config` objects:

```json
{
  "agents": {
    "opencode": {
      "working_directory": "/workspace/bastion",
      "auth": {
        "anthropic": {
          "type": "api",
          "key": "${{ secret.ANTHROPIC_API_KEY }}"
        }
      },
      "config": {
        "model": "anthropic/claude-sonnet-4-20250514"
      }
    }
  }
}
```

For other providers, update both objects. Also update or remove the
`config` agent model and variant overrides when they should not use OpenAI
`gpt-5.5` with `xhigh`.

Secret references such as `${{ secret.OPENAI_API_KEY }}` are resolved by
Bastion before guest setup runs. The resolved API key is written into the guest
OpenCode auth file, so use a token scoped for these environments.
