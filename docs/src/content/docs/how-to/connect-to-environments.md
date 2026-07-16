---
title: Connect to environments
description: Connect to a running Bastion environment with SSH, OpenCode, or Bastion mux.
---

Bastion proxies environment connections through the configured host or cluster
API. You do not need the guest IP address or SSH private-key path.

## Prerequisites

Before you connect:

- Confirm that the environment status is `running`.
- Ensure your client can reach the configured Bastion API.
- Install `opencode` locally before using `bastion opencode`.
- Install `tmux` locally before using `bastion mux`.

Check the environment:

```sh
ENVIRONMENT_KEY="ENVIRONMENT_KEY"
bastion env get --key "$ENVIRONMENT_KEY"
```

Replace `ENVIRONMENT_KEY` with the environment key. If the response has
`status: "error"`, inspect `lastError` before reconnecting.

## Open an interactive SSH shell

1. Connect by environment key:

   ```sh
   bastion ssh --key "$ENVIRONMENT_KEY"
   ```

2. Work in the guest shell. Bastion requests a PTY, forwards terminal resizes,
   and restores your local terminal when the session ends.

3. Run `exit` to disconnect.

For an unkeyed environment, use:

```sh
ENVIRONMENT_ID="ENVIRONMENT_ID"
bastion ssh --id "$ENVIRONMENT_ID"
```

Replace `ENVIRONMENT_ID` with the generated `env_` ID.

## Run one command over SSH

1. Put `--` before the guest command so guest flags are not parsed as Bastion
   flags. Bastion joins command elements after `--` with spaces. For a compound
   command, pass one complete quoted remote shell string so your local shell
   does not consume operators such as `&&`:

   ```sh
   bastion ssh --key "$ENVIRONMENT_KEY" -- "cd /workspace/project && git status --short"
   ```

2. Read guest stdout and stderr from the corresponding local streams. If the
   guest command exits nonzero, the Bastion CLI also exits nonzero.

## Attach OpenCode

1. Confirm that OpenCode is available on your client:

   ```sh
   opencode --version
   ```

2. Attach the local TUI to the OpenCode server in the environment:

   ```sh
   bastion opencode --key "$ENVIRONMENT_KEY"
   ```

   Bastion runs `opencode attach` against the environment's proxied agent URL.
   The template must define `agents.opencode`, and the environment must be
   running.

## Switch between environments with mux

1. Start or attach to the Bastion tmux session:

   ```sh
   bastion mux
   ```

2. Select an environment from the menu, then select `SSH` or `OpenCode`.

3. Create another tmux window with `Ctrl-b c` to connect to another
   environment. Windows use the environment key as their name, or the generated
   ID when no key exists.

`bastion mux` requires an interactive terminal. It lists environments from the
currently configured API and namespace.

## Connect through a remote API

:::danger
The host and cluster APIs provide no native authentication or TLS. Anyone who
can reach an API can manage resources, retrieve secrets, enter environments, and
proxy services. Do not expose a Bastion API directly to an untrusted network.
:::

1. Put the API behind a transparently authenticated private network such as
   Tailscale. For one private-network option, follow
   [Remote access with Tailscale](/how-to/remote-access-with-tailscale/).

   The Bastion CLI cannot supply arbitrary authentication headers or a configured
   client certificate. Browser-oriented SSO challenges therefore do not work as
   a general CLI authentication layer. If you use an HTTP reverse proxy for TLS
   termination, enforce access transparently at the network boundary and
   configure the proxy to pass HTTP upgrade traffic and long-lived streams used
   by SSH, OpenCode, and tunnels.

2. Persist the protected API URL on the client:

   ```sh
   BASTION_API_URL="https://bastion.example.com"
   bastion client set api-url "$BASTION_API_URL"
   bastion client config
   ```

3. If you connect to a cluster, also select exactly one namespace:

   ```sh
   TEAM_KEY="TEAM_KEY"
   bastion client set namespace-key "$TEAM_KEY"
   bastion client config
   ```

   Replace `TEAM_KEY` with the cluster namespace key.

4. Use the normal connection commands:

   ```sh
   ENVIRONMENT_KEY="ENVIRONMENT_KEY"
   bastion ssh --key "$ENVIRONMENT_KEY"
   bastion opencode --key "$ENVIRONMENT_KEY"
   ```

5. To return to the default local host API and no namespace, remove the
   persisted overrides:

   ```sh
   bastion client remove namespace-key
   bastion client remove api-url
   bastion client config
   ```

macOS Apple silicon supports these client workflows and can also run the cluster
control plane. It cannot host Bastion VMs; connect it to a Linux x86_64 VM host
or cluster.

See [Expose environment services](/how-to/expose-environment-services/) for web
applications and the [host CLI reference](/reference/cli/host/) for connection
flags. Review [Security and operational limits](/explanation/security-and-operational-limits/)
before enabling remote API access.
