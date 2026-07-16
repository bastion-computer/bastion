---
title: Get started
description: Install Bastion on one Linux host, create an environment, and connect to it.
---

In this tutorial, you install Bastion on one Linux VM host, prepare its shared
base, create a template, launch an environment, and run a command inside it.
The result is a disposable virtual computer that prints `hello from Bastion`.

## Prerequisites

Use a Linux x86_64 host with:

- `systemd` and `sudo`.
- Read and write access to `/dev/kvm`.
- `/dev/vhost-vsock`.
- Internet access for release, Ubuntu image, Cloud Hypervisor, and OpenCode
  downloads.
- At least 2 available vCPUs, 2 GiB of memory, and 20 GiB of disk for the
  tutorial environment, plus capacity for the host.

If the Linux host is itself a VM, enable nested virtualization in its provider.
macOS Apple silicon can run the Bastion client and cluster control plane, but it
cannot run the host API, daemon, or virtual machines used in this tutorial.

## Check the host

1. Confirm that the host architecture is x86_64:

   ```sh
   uname -m
   ```

   The command must print `x86_64`.

2. Confirm that KVM is accessible and vhost-vsock exists:

   ```sh
   test -r /dev/kvm && test -w /dev/kvm
   test -e /dev/vhost-vsock
   ```

   Both commands complete without output when the host is ready.

## Install Bastion

:::caution
The installer downloads release binaries, uses `sudo` to write under
`/usr/local/bin` and `/etc`, and enables two system services. Review
`https://bastion.computer/install.sh` first if your security policy requires it.
:::

1. Install the latest stable release:

   ```sh
   curl -fsSL https://bastion.computer/install.sh | bash
   ```

2. Confirm the installed version and services:

   ```sh
   bastion version
   sudo systemctl is-active bastiond.service bastion-api.service
   ```

   The service command prints `active` twice. The host API listens on
   `localhost:3148`; the privileged daemon listens on
   `/run/bastion/bastiond.sock`.

## Prepare the host

:::caution
The next command downloads runtime assets and installs missing operating-system
packages through a supported package manager. It can invoke `sudo`.
:::

1. Install the Cloud Hypervisor and OpenCode assets and required utilities:

   ```sh
   bastion system init --with-utilities
   ```

2. Verify every dependency:

   ```sh
   bastion system check
   ```

   Every leaf in the dependency tree must end in `[ok]`. The check includes
   Linux x86_64, KVM access, `/dev/vhost-vsock`, host utilities, and pinned
   runtime assets.

3. Verify the host API:

   ```sh
   curl -fsS http://localhost:3148/v1/health
   ```

   Expected response:

   ```json
   { "status": "ok" }
   ```

## Build the base

1. Build the shared, template-independent base image:

   ```sh
   bastion base build
   ```

   Bastion streams build progress to stderr and prints metadata to stdout. The
   `contentAddress` begins with `sha256:`.

2. Confirm the stored base:

   ```sh
   bastion base get
   ```

   Bastion keeps one base per host. You reuse this base for later templates.

## Create a template

1. Create `template.json` in your current directory:

   ```sh
   cat > template.json <<'JSON'
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
           "run": "mkdir -p /workspace && printf 'hello from Bastion\\n' > /workspace/README.md"
         }
       ]
     }
   }
   JSON
   ```

2. Create an immutable template with the key `get-started`:

   ```sh
   bastion templates create --key get-started --file ./template.json
   ```

   Template creation boots a temporary VM, runs `actions.init`, and stores its
   disk overlay. Values in the following example are placeholders:
   `TEMPLATE_ID` is the generated template ID, `BASE_CONTENT_ADDRESS` is the
   base digest, and `ISO_TIMESTAMP` is the creation time.

   ```json
   {
     "id": "TEMPLATE_ID",
     "key": "get-started",
     "baseContentAddress": "BASE_CONTENT_ADDRESS",
     "createdAt": "ISO_TIMESTAMP"
   }
   ```

## Launch an environment

1. Create an environment with the key `get-started-1`:

   ```sh
   bastion env create --template-key get-started --key get-started-1 --tag tutorial
   ```

   Bastion cold-boots a writable overlay and waits until it is ready. In this
   example output, `ENVIRONMENT_ID`, `TEMPLATE_ID`, and the timestamps are
   generated values:

   ```json
   {
     "id": "ENVIRONMENT_ID",
     "key": "get-started-1",
     "status": "running",
     "templateId": "TEMPLATE_ID",
     "tags": ["tutorial"],
     "createdAt": "ISO_TIMESTAMP",
     "updatedAt": "ISO_TIMESTAMP"
   }
   ```

2. Run a command in the environment:

   ```sh
   bastion ssh --key get-started-1 -- cat /workspace/README.md
   ```

   Expected output:

   ```text
   hello from Bastion
   ```

3. Open an interactive shell if you want to explore the VM:

   ```sh
   bastion ssh --key get-started-1
   ```

   Run `exit` to leave the shell.

## Clean up

:::caution
Removing an environment permanently deletes its writable disk. The later
commands also delete the prepared template and local JSON file. Preserve
anything you need before you continue.
:::

1. Remove the environment before its template:

   ```sh
   bastion env remove --key get-started-1
   ```

2. Remove the template:

   ```sh
   bastion templates remove --key get-started
   rm ./template.json
   ```

   Keep the base so you can create other templates without rebuilding it.

## Next steps

- [Run parallel agents with the demo repository](/tutorials/run-parallel-agents/).
- [Manage environments](/how-to/manage-environments/).
- [Connect to environments](/how-to/connect-to-environments/).
- [Understand the resource lifecycle](/explanation/resource-lifecycle/).
- [Read the host CLI reference](/reference/cli/host/).
- [Review host requirements and configuration](/reference/host-requirements-and-configuration/).
