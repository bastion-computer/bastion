---
title: Bastion Dev Environment
description: An example of how we use Bastion to build Bastion.
---

This example creates a development environment for working on Bastion itself. It
installs mise, configures GitHub CLI, configures OpenCode, clones the Bastion
repository, installs the repository tools, pulls the latest changes when each
environment starts, and opens interactive SSH shells in the repository
directory.

## Template

Create `template.json`:

```json title="template.json"
{
  "resources": {
    "vcpu": 4,
    "memory": 8,
    "volume": 40
  },
  "actions": {
    "init": [
      {
        "use": "setup_mise"
      },
      {
        "use": "setup_github_cli",
        "with": {
          "token": "${{ env.GITHUB_TOKEN }}",
          "hostname": "github.com",
          "git_protocol": "https"
        }
      },
      {
        "use": "setup_opencode",
        "with": {
          "auth": "{\"openai\":{\"type\":\"api\",\"key\":\"${{ env.OPENAI_API_KEY }}\"}}",
          "config": "{\"model\":\"openai/gpt-5.5\",\"permission\":\"allow\",\"agent\":{\"build\":{\"model\":\"openai/gpt-5.5\",\"variant\":\"xhigh\"},\"plan\":{\"model\":\"openai/gpt-5.5\",\"variant\":\"xhigh\"}}}"
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

Export the host environment variables referenced by the template before creating
the environment:

```sh
export GITHUB_TOKEN=ghp_xxxxxx
export OPENAI_API_KEY=sk_xxxxxx
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

The template uses OpenAI by default. To use Anthropic instead, change the
`setup_opencode` auth and config JSON, then export `ANTHROPIC_API_KEY` on the
host before creating the environment:

```json
{
  "use": "setup_opencode",
  "with": {
    "auth": "{\"anthropic\":{\"type\":\"api\",\"key\":\"${{ env.ANTHROPIC_API_KEY }}\"}}",
    "config": "{\"model\":\"anthropic/claude-sonnet-4-20250514\"}"
  }
}
```

For other providers, update both JSON strings. Also update or remove the
`config` agent model and variant overrides when they should not use OpenAI
`gpt-5.5` with `xhigh`.

Environment substitutions such as `${{ env.OPENAI_API_KEY }}` are resolved by
Bastion when the template is created. The resolved API key is written into the
prepared guest OpenCode auth file, so use a token scoped for environments cloned
from this template.
