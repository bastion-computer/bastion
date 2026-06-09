---
title: Remote access with Tailscale
description: Access a Bastion host API remotely through Tailscale Serve.
---

Tailscale Serve can publish the Bastion host API to your private tailnet without
changing Bastion's listen address. Bastion stays bound to `localhost:3148`, and
Tailscale terminates HTTPS for other devices in your tailnet.

Use this when Bastion runs on a Linux host, but you want to drive it from another
machine with the `bastion` CLI.

## Requirements

You need:

- Bastion installed and running on the Linux host.
- Tailscale installed and connected on the Bastion host.
- Tailscale installed and connected on the remote client machine.
- Tailscale Serve enabled for your tailnet.

If your tailnet uses ACLs, make sure the remote client can connect to the
Bastion host's Serve HTTPS port.

## Check the Bastion Host

On the Linux host, confirm the Bastion services are running:

```sh
sudo systemctl status bastiond.service bastion-api.service
```

Check that the host API is reachable locally:

```sh
curl -fsS http://localhost:3148/v1/health
```

The response should be:

```json
{ "status": "ok" }
```

## Publish the API with Tailscale Serve

On the Bastion host, proxy the local API through Tailscale Serve:

```sh
tailscale serve --bg localhost:3148
```

The `--bg` flag keeps the Serve configuration running after the command exits and
across Tailscale restarts.

Print the Serve URL:

```sh
tailscale serve status
```

The output includes a URL like:

```text
https://bastion-host.example.ts.net
```

That URL is the Bastion API base URL for remote clients.

## Configure the Remote CLI

On the remote client machine, verify that the Tailscale URL reaches the Bastion
API:

```sh
curl -fsS https://bastion-host.example.ts.net/v1/health
```

Then point the `bastion` CLI at that URL:

```sh
bastion client set api-url https://bastion-host.example.ts.net
```

Confirm the resolved client configuration:

```sh
bastion client config
```

You can also skip the persisted setting and pass the URL per command:

```sh
bastion --api-url https://bastion-host.example.ts.net env list
```

## Use Bastion Remotely

After the remote CLI points at the Serve URL, normal Bastion commands use the
remote host API:

```sh
bastion templates list
bastion env list
```

Create environments from the remote client the same way you would on the host:

```sh
bastion env create --template-key hello --key remote-hello
```

SSH and OpenCode also go through the same API URL:

```sh
bastion ssh --key remote-hello
bastion opencode --key remote-hello
```

## Stop Serving Bastion

To remove the Tailscale Serve configuration from the host:

```sh
tailscale serve reset
```

If you configured the remote client with a persisted API URL, remove it when you
want to go back to the local default:

```sh
bastion client remove api-url
```

## Security Notes

Keep `BASTION_ADDR` bound to `localhost:3148` unless you intentionally want the
host API reachable outside the machine. Tailscale Serve can proxy localhost, so
you do not need to bind Bastion to `0.0.0.0`.

Access to the Bastion API allows users to create, remove, SSH into, and attach to
environments on that host. Restrict the Bastion host with Tailscale ACLs to only
the users and devices that should manage those environments.

Tailscale Serve is private to your tailnet. Do not use Tailscale Funnel for the
Bastion API unless you have added a separate authentication and authorization
layer in front of Bastion.
