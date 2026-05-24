---
title: Introduction
description: Bastion replicates agentic coding environments into isolated VMs.
---

Bastion is an orchestration tool for running many agentic coding environments on
your own Linux infrastructure. Each environment is created from a declarative
template and runs inside its own Cloud Hypervisor VM, separate from the host and
from every other environment.

The launch scope is intentionally small:

| Concept      | What it does                                                           |
| ------------ | ---------------------------------------------------------------------- |
| Templates    | JSON definitions for VM resources and ordered initialization actions.  |
| Environments | Running VM instances created from templates.                           |
| Actions      | Built-in and custom setup steps such as `setup_node` and `setup_mise`. |
| SSH          | Interactive shell and command execution inside running environments.   |
| System setup | Host checks and Cloud Hypervisor asset installation.                   |

## Architecture

Bastion is split into two processes.

| Process         | Role                                                                                                         |
| --------------- | ------------------------------------------------------------------------------------------------------------ |
| `bastion start` | Runs the local host API on `localhost:3148` by default, stores metadata in SQLite, and serves the CLI.       |
| `bastiond`      | Runs privileged Cloud Hypervisor operations behind a Unix socket at `/run/bastion/bastiond.sock` by default. |

The regular `bastion` CLI talks to the host API. The host API talks to
`bastiond` only when it needs to launch, inspect, remove, or connect to a VM.
This keeps the public API local and narrow while the privileged runtime work
stays behind a Unix socket.

## Environments

An environment is a Cloud Hypervisor VM with its own guest kernel, root file
system, process tree, networking, and SSH server. Environments are meant to give
coding agents enough control to install packages, run background services, bind
ports, and modify files without colliding with other agents on the same host.

The default VM allocation is 2 vCPU, 2 GiB memory, and a 20 GiB root volume.
Templates can override those values per environment.

## Templates

Templates are immutable JSON records. A template defines optional VM resources
and required `actions.init` steps. Init actions run once when an environment is
created.

Actions can be inline shell commands:

```json
{
  "actions": {
    "init": [{ "run": "mkdir -p /workspace" }]
  }
}
```

Actions can also reference built-in or custom action packages:

```json
{
  "actions": {
    "init": [{ "use": "setup_node", "with": { "version": 24 } }]
  }
}
```

## Start Here

Use the [Quick Start](/quick-start/) to install Bastion, prepare the host, create
a template, launch an environment, and connect over SSH.
