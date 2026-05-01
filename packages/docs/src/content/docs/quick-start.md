---
title: Quick Start
description: A quick guide to deploy your first agents with bastion.
---

Bastion provides developers with a platform for deploying, running, and scaling their AI agents. This guide will take you from zero to one while introducing a few key concepts.

:::note
_This guide assumes access to an `ANTHROPIC_API_KEY`. Bastion supports all
frontier models and you can substitute this value with your preferred
provider._
:::

## Installation

The most convenient way to install bastion is with the following one liner.

```sh
curl -fsSL https://bastion.computer/install.sh | bash
```

Then start the server which will listen on `localhost` port `3148` by default. For this guide, we will make sure the bastion process has access to the `ANTHROPIC_API_KEY`.

```sh
ANTHROPIC_API_KEY="sk..." bastion start
```

> _For downloading raw binaries, building from source, or using a package manager see the extended [installation]() guide._

## Bind a secret reference

Bastion has a system for obfuscating environment variables so that they cannot be directly accessed within the sandbox. Rather than passing secrets to the sandbox, they are given a substituted value that gets intercepted and replaced by the host on outbound requests. This protects secrets from exfiltration risk.

```sh
bastion secrets bind SBX_ANTHROPIC_API_KEY:ANTHROPIC_API_KEY \
    --allow-host "*.anthropic.com"
```

```json
{
  "id": "sec_xxxxxx",
  "key": "SBX_ANTHROPIC_API_KEY",
  "env": "ANTHROPIC_API_KEY",
  "allowHosts": ["*.anthropic.com"],
  "createdAt": "<iso_timestamp>"
}
```

We are creating a secret reference (`SBX_ANTHROPIC_API_KEY`) that maps to a host environment variable (`ANTHROPIC_API_KEY`).

> _See the extended guide on [secrets](./guides/secrets.md) for all available commands and options_.

## Define a template

All agents running on the bastion platform execute within a secure sandbox that is isolated from your host system. This isolation is backed by a [Firecracker microVM](https://firecracker-microvm.github.io/) which can be booted in milliseconds and gives your agents full access to their own Linux environment.

Rather than configuring every sandbox from scratch, bastion provides a declarative high level JSON schema for defining a VM environment.

```sh
bastion templates create --config '{
  "harness": {
    "type": "opencode",
    "provider": {
      "anthropic": {
        "options": {
          "apiKey": "${{ secrets.SBX_ANTHROPIC_API_KEY }}"
        }
      }
    }
  }
}'
```

```json
{
  "id": "tpl_xxxxxx",
  "createdAt": "<iso_timestamp>"
}
```

This is the simplest configuration we can have. It is a minimal VM with an Anthropic authenticated [opencode](https://opencode.ai/docs/) harness. In practice, you will have many more options available, but for simplicity we'll keep it bare bones here.

As mentioned previously, only a placeholder API key is passed into the sandbox. Bastion will intercept egress calls and replace this with the real value on the host layer.

> _See the extended guide on [templates]() for all sandbox customization options._

## Deploy the agent

We now have everything we need to create a sandbox.

```sh
bastion sandbox create --template tpl_xxxxxx
```

```json
{
  "id": "sbx_xxxxxx",
  "status": "pending",
  "source": {
    "type": "template",
    "id": "tpl_xxxxxx"
  },
  "createdAt": "<iso_timestamp>"
}
```

This action will asynchronously initialize a new Firecracker microVM with our configured template. We can then use the following action to check on the status of our sandbox.

```sh
bastion sandbox list
```

```json
[
  {
    "id": "sbx_xxxxxx",
    "status": "running",
    "source": {
      "type": "environment",
      "id": "env_xxxxxx"
    },
    "createdAt": "<iso_timestamp>"
  }
]
```

## Interact with the agent

Once the sandbox is running you can start an opencode TUI using the following command. Since the agent is running in an isolated VM, it won't have access to anything on the host machine.

```sh
bastion exec --id "sbx_xxxxxx" opencode
```

For an actual coding agent use case, you would also want to set up your templates with code repositories and necessary tooling to spin up a dev server.

> _See the extended [connection]() guide for more details on interacting with running sandboxes._

## Sandbox snapshots

Sandboxes can also be created from a snapshot. This can be useful for a variety of reasons such as branching parallel workflows from a certain checkpoint or restoring a session from a previous state.

1. Pause the sandbox

```sh
bastion sandbox pause sbx_xxxxxx
```

```json
{
  "id": "sbx_xxxxxx",
  "status": "paused",
  "source": {
    "type": "template",
    "id": "tpl_xxxxxx"
  },
  "createdAt": "<iso_timestamp>"
}
```

2. Create a new snapshot

```sh
bastion sandbox snapshot sbx_xxxxxx
```

```json
{
  "id": "snp_xxxxxx",
  "sandbox": "sbx_xxxxxx",
  "status": "pending",
  "createdAt": "<iso_timestamp>"
}
```

3. Verify snapshot completed successfully

```sh
bastion sandbox snapshot list
```

```json
[
  {
    "id": "snp_xxxxxx",
    "sandbox": "sbx_xxxxxx",
    "status": "ok",
    "createdAt": "<iso_timestamp>"
  }
]
```

4. Initialize a new sandbox from the resulting snapshot

```sh
bastion sandbox create --snapshot snp_xxxxxx
```

```json
{
  "id": "sbx_yyyyyy",
  "status": "pending",
  "source": {
    "type": "snapshot",
    "id": "snp_xxxxxx"
  },
  "createdAt": "<iso_timestamp>"
}
```

5. Get a list of all sandboxes

```sh
bastion sandbox list
```

```json
[
  {
    "id": "sbx_xxxxxx",
    "status": "paused",
    "source": {
      "type": "environment",
      "id": "env_xxxxxx"
    },
    "createdAt": "<iso_timestamp>"
  },
  {
    "id": "sbx_yyyyyy",
    "status": "running",
    "source": {
      "type": "snapshot",
      "id": "snp_xxxxxx"
    },
    "createdAt": "<iso_timestamp>"
  }
]
```

> _See the extended guide on [sandboxes]() for all available actions to manage VM lifecycles._

## Summary

In this guide we ran through a "hello world" example of deploying parallel agents with bastion.

- **Installation**: setup the bastion service and cli via a single shell command.
- **Secrets**: bound host environment variables to secret references that are resolved on outbound calls.
- **Templates**: used a declarative JSON schema to configure an agent's operating environment.
- **Sandboxes**: initialized isolated Firecracker microVMs based on the defined template.
- **Snapshots**: captured VM state and cloned it to scale out parallel workflows.

From here, dive into the extended guides for deeper coverage on each topic.
