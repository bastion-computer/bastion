---
title: SSH
description: Connect to Bastion environments interactively or run commands.
---

Bastion exposes environment SSH through the host API. The CLI accepts an
environment ID or key and does not require you to know the guest IP address or
SSH key path.

## Interactive Shell

Open a shell in a running environment:

```sh
bastion ssh --id env_xxxxxx
```

When stdin and stdout are terminals, Bastion requests a PTY, forwards terminal
resizes, and restores your terminal mode when the session exits.

## Run a Command

Run a command and return its exit status:

```sh
bastion ssh --id env_xxxxxx -- uname -a
```

If the environment has a key, you can reference it by key instead:

```sh
bastion ssh --key dev-env -- uname -a
```

Use `--` before commands that include flags:

```sh
bastion ssh --id env_xxxxxx -- sh -lc 'cd /workspace && ls -la'
```

Stdout and stderr from the guest are forwarded separately to the local process.
If the remote command exits non-zero, the CLI exits non-zero with the same code.

## Persistent Sessions

Use `bastion mux` when you want SSH sessions to keep running after you close or
detach from the local terminal:

```sh
bastion mux
```

The mux TUI uses `tmux` as the process backend. It shows existing Bastion SSH
sessions as tabs and lets you create a new tab from currently `running` or
`paused` environments. Detaching from the TUI does not close those SSH sessions,
so commands and agent processes inside the guest can continue running.

Useful keys:

| Key        | Action                                      |
| ---------- | ------------------------------------------- |
| `n`        | Create the first SSH session.               |
| `ctrl+b n` | Choose another environment to connect.      |
| `ctrl+b h` | Switch to the previous SSH session.         |
| `ctrl+b l` | Switch to the next SSH session.             |
| `ctrl+b d` | Detach from the mux TUI and leave SSH open. |

The command checks that `tmux` is available before starting and exits without
changing environments if it is missing.

## Connection Requirements

SSH requires an environment with stored SSH metadata and a VM state that can be
connected to. In normal use, wait for the environment to reach `running` before
connecting.

Check status with:

```sh
bastion env get --id env_xxxxxx
```

If the environment is `error`, inspect `lastError` and remove or recreate the
environment after fixing the underlying template or host issue.

## How It Works

The guest is provisioned with root SSH access using the SSH key generated during
Cloud Hypervisor asset installation. The host API upgrades the HTTP connection
using the `bastion-ssh` protocol and multiplexes stdin, stdout, stderr, terminal
resize, exit status, and error frames over that connection.

Direct guest SSH is an implementation detail. Prefer `bastion ssh` so the CLI
continues to work as VM networking and metadata handling evolve.
