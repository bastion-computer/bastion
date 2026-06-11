---
title: Quick Start
description: Install Bastion, launch an environment, and connect over SSH or OpenCode.
---

This guide takes you from a fresh Linux host to a running Bastion environment.

## Requirements

Bastion currently targets Linux hosts with x86_64 KVM support.

Check that `/dev/kvm` exists and that your user can read and write it:

```sh
ls -l /dev/kvm
```

If your host is a cloud VM, make sure nested virtualization is enabled for the
instance type.

## Install Bastion

Install `bastion`, `bastiond`, and the systemd services:

```sh
curl -fsSL https://bastion.computer/install.sh | bash
```

The installer creates and starts `bastion-api.service` and `bastiond.service` by
default. It also seeds `/etc/default/bastion` with service environment values
such as `BASTION_DATA_DIR`, `BASTION_ADDR`, and `BASTIOND_SOCKET`. Edit that file
to customize service settings; future installer runs preserve it.

On macOS Apple silicon, the same installer installs the client-only `bastion`
CLI. Use `bastion --api-url` to connect it to a remote Linux Bastion host API.

If you only want the binaries, download the release archive from the
[GitHub releases page](https://github.com/bastion-computer/bastion/releases).

## Prepare the Host

Check host dependencies:

```sh
bastion system check
```

Install Cloud Hypervisor assets and any missing supported utilities:

```sh
bastion system add cloud-hypervisor --with-utilities
```

This downloads the Cloud Hypervisor binary, guest kernel, guest initramfs, root
file system image, and SSH key into the Bastion data directory. The default data
directory is `~/.bastion`.

## Check Bastion

The host API listens on `localhost:3148` by default. The daemon listens on the
Unix socket `/run/bastion/bastiond.sock` by default.

Check that both services are running:

```sh
sudo systemctl status bastiond.service bastion-api.service
```

## Create a Template

Create a template file:

```json title="template.json"
{
  "resources": {
    "vcpu": 2,
    "memory": 2,
    "volume": 20
  },
  "agents": {
    "opencode": {}
  },
  "actions": {
    "init": [
      {
        "run": "mkdir -p /workspace && printf 'hello from bastion\\n' | tee /workspace/README.md"
      }
    ]
  }
}
```

Register it with Bastion:

```sh
bastion templates create --key hello --file ./template.json
```

Template creation boots a temporary VM, runs `actions.init`, and stores a Cloud
Hypervisor snapshot plus an immutable prepared root disk. Init logs stream to
stderr while the final template metadata is written to stdout.

The `hello` key is optional, but it gives the template a stable human-friendly
name for later commands.

Example response:

```json
{
  "id": "tpl_xxxxxx",
  "key": "hello",
  "createdAt": "<iso_timestamp>"
}
```

## Launch an Environment

Create an environment from the template:

```sh
bastion env create --template-key hello --tag quickstart
```

Environment creation restores the prepared template snapshot, creates a small
qcow2 copy-on-write root disk overlay, gets a fresh DHCP lease, runs any
`actions.start` steps, and prints the final JSON environment record to stdout.

Example response:

```json
{
  "id": "env_xxxxxx",
  "status": "running",
  "templateId": "tpl_xxxxxx",
  "tags": ["quickstart"],
  "createdAt": "<iso_timestamp>",
  "updatedAt": "<iso_timestamp>"
}
```

## Connect Over SSH

Run a command inside the environment:

```sh
bastion ssh --id env_xxxxxx -- cat /workspace/README.md
```

Open an interactive shell:

```sh
bastion ssh --id env_xxxxxx
```

## Connect With OpenCode

If you have `opencode` installed locally, attach it to the environment's OpenCode
server through the Bastion API proxy:

```sh
bastion opencode --id env_xxxxxx
```

## Clean Up

Remove the environment when you are done:

```sh
bastion env remove --id env_xxxxxx
```

Remove the template if you do not need it anymore:

```sh
bastion templates remove --key hello
```

## Next Steps

Read the [Templates guide](/guides/templates/) to define reusable environments,
the [Environments guide](/guides/environments/) to manage VM lifecycle, and the
[Custom Actions guide](/ecosystem/custom-actions/) to package shared setup
steps.

For a full practical walkthrough, use the
[issue tracker demo repo](/examples/bastion-demo-repo/) to create parallel Bun
and TypeScript coding environments.
