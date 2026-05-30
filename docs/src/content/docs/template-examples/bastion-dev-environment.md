---
title: Bastion Dev Environment
description: An example of how we use Bastion to build Bastion.
---

This example creates a development environment for working on Bastion itself. It
installs mise, configures GitHub CLI, configures OpenCode, clones the Bastion
repository, installs the repository tools, and opens interactive SSH shells in
the repository directory.

## Template

Create `template.json`:

```json title="template.json"
{
  "$schema": "https://bastion.computer/schemas/template.json",
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
          "provider": "openai",
          "model": "openai/gpt-5",
          "api_key": "${{ env.OPENAI_API_KEY }}",
          "permission": "allow"
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
        "run": "printf '\nif [ -d /workspace/bastion ]; then\n  cd /workspace/bastion\nfi\n' >> /root/.bashrc"
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

The shell starts in `/workspace/bastion` because the template appends a guarded
`cd /workspace/bastion` to `/root/.bashrc`.

## Change OpenCode Providers

The template uses OpenAI by default. To use Anthropic instead, change the
`setup_opencode` action and export `ANTHROPIC_API_KEY` on the host before
creating the environment:

```json
{
  "use": "setup_opencode",
  "with": {
    "provider": "anthropic",
    "model": "anthropic/claude-sonnet-4-20250514",
    "api_key": "${{ env.ANTHROPIC_API_KEY }}"
  }
}
```

For other providers, update `provider`, `model`, and `api_key`. Add `base_url`
when the provider needs a custom API endpoint.

Environment substitutions such as `${{ env.OPENAI_API_KEY }}` are resolved by
Bastion when the environment is created. The resolved API key is written into the
guest OpenCode auth file, so use a token scoped for this environment.
