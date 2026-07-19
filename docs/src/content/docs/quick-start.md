---
title: Quick Start
description: Install Bastion, launch an environment, and connect over SSH or OpenCode.
---

This guide takes you from a fresh Linux host to a running Bastion environment.

## Requirements

Bastion currently targets Linux hosts with x86_64 KVM support.

Check that `/dev/kvm` exists, your user can read and write to it, and that the
host exposes `/dev/vhost-vsock` for VM tunnel traffic:

```sh
ls -l /dev/kvm
ls -l /dev/vhost-vsock
```

If your host is a cloud VM, make sure nested virtualization is enabled for the
instance type.

## Install Bastion

Install Bastion with the following script:

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

To test the latest release candidate before it becomes the latest stable
release, pass `--experimental`:

```sh
curl -fsSL https://bastion.computer/install.sh | bash -s -- --experimental
```

## Prepare the Host

Check host dependencies:

```sh
bastion system check
```

Install Cloud Hypervisor, OpenCode assets, and any missing utilities:

```sh
bastion system init --with-utilities
```

This downloads all the required assets to operate a Bastion environment into the
Bastion data directory. The default data directory is `~/.bastion`.

Run the system check again to ensure all dependencies were installed:

```sh
bastion system check
```

## Check Bastion

The host API listens on `localhost:3148` by default. The daemon listens on the
Unix socket `/run/bastion/bastiond.sock` by default.

Check that both services are running:

```sh
sudo systemctl status bastiond.service bastion-api.service
```

## Build the Base

Build the shared base image that will be use to back all downstream templates and
environments:

```sh
bastion base build
```

The base contains common guest components used by every template. Bastion builds
it once, then creates lightweight template overlays on top. See the [Base guide](/guides/base/)
for inspection, backup, import, and replacement workflows.

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

Template creation boots a temporary VM from the base, runs `actions.init`, and
stores the resulting immutable qcow2 overlay. Init logs stream to stderr while
the final template metadata is written to stdout.

The `hello` key is optional, but it gives the template a stable human-friendly
name for later commands.

Example response:

```json
{
  "id": "tpl_xxxxxx",
  "key": "hello",
  "baseContentAddress": "sha256:...",
  "createdAt": "<iso_timestamp>"
}
```

## Launch an Environment

Create an environment from the template:

```sh
bastion env create --template-key hello --tag quickstart
```

Environment creation adds a writable qcow2 overlay backed by the immutable template,
cold-boots it with fresh cloud-init state, runs any `actions.start` steps, and prints
the final JSON environment record to stdout.

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

Run a command inside the environment (replace `env_xxxxxx` with the real id):

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

Read the [base guide](/guides/base/) to manage the shared image, the
[templates guide](/guides/templates/) to define reusable environments, the
[environments guide](/guides/environments/) to manage VM lifecycle, and the
[custom actions guide](/actions/custom-actions/) to package shared setup steps.

For a full practical walkthrough, use the [issue tracker demo repo](/examples/bastion-demo-repo/)
to create parallel Bun and TypeScript coding environments.
