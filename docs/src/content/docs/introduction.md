---
title: Introduction
description: Self-hosted virtual machines for background coding agents.
---

Bastion is an orchestration tool for running background coding agents on
your own Linux infrastructure. Each environment is created from a declarative
template and runs inside its own Cloud Hypervisor VM, separate from the host and
from every other environment.

Key concepts in Bastion center around isolating a coding agent and dev environment
within its own VM.

| Concept      | What it does                                                        |
| ------------ | ------------------------------------------------------------------- |
| Base         | Shared base image applied to all VMs.                               |
| Templates    | A JSON to define a dev environment built on top of the shared base. |
| Environments | A running instance of a dev environment built on top of a template. |

The following diagram shows how these three concepts relate. Environments are the running VMs.

```
base image
├── template 1
│   ├── environment 1
│   └── environment 2
├── template 2
│   ├── environment 1
│   └── environment 2
└── template 3
    ├── environment 1
    └── environment 2
```

## Runtime

Bastion is split into two processes.

| Process                | Role                                                                                                                |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------- |
| `bastion start api`    | Runs the local host API on `localhost:3148` by default, stores metadata in SQLite, and serves clients like the CLI. |
| `bastion start daemon` | Runs privileged Cloud Hypervisor operations behind a Unix socket at `/run/bastion/bastiond.sock` by default.        |

The regular `bastion` CLI talks to the host API. The host API talks to
the daemon only when it needs to launch, inspect, remove, or connect to a VM.
This keeps the public API local and narrow while the privileged runtime work
stays behind a Unix socket.

## Templates

Templates are immutable overlays backed by a prepared base image. A template defines
VM resources, coding agents such as opencode, and lifecycle actions for initialization
and start. Init actions run once when the template is created. Start actions run
each time an environment (i.e. VM) cold-boots from a fresh overlay backed by the
template.

Actions can be inline shell commands:

```json
{
  // full config hidden for brevity..

  "actions": {
    "init": [{ "run": "mkdir -p /workspace" }]
  }
}
```

Actions can also reference built-in or custom action packages:

```json
{
  // full config hidden for brevity..

  "actions": {
    "init": [{ "use": "setup_node", "with": { "version": 24 } }]
  }
}
```

## Environments

An environment is a Cloud Hypervisor VM with its own guest kernel, root file
system, process tree, and networking. Environments are meant to give coding
agents enough control to install packages, run background services, bind
ports, and modify files without colliding with the host and other agents.

The default VM allocation is 2 vCPU, 2 GiB memory, and a 20 GiB root volume.
Templates can override those values per environment.

## Start Here

Use the [Quick Start](/quick-start/) to install Bastion, [prepare the host](/guides/system)
and [base](/guides/base/), create a [template](/guides/templates), launch an
[environment](/guides/environments), and connect over SSH.
