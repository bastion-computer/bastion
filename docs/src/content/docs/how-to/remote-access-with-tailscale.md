---
title: Access Bastion with Tailscale
description: Reach a localhost-bound Bastion host API privately through Tailscale Serve.
---

Use Tailscale Serve to publish a Linux host's Bastion API to your tailnet while
Bastion remains bound to `localhost:3148`. Tailscale terminates HTTPS; tailnet
policy controls which devices can reach the Serve URL.

## Prerequisites

Before you begin:

- Install and start Bastion on a Linux x86_64 VM host.
- Install Tailscale and join the same tailnet on the Bastion host and client.
- Enable Tailscale Serve for the tailnet.
- Allow the client to reach the host's Serve HTTPS port in your tailnet policy.
- Install the Bastion CLI on the client. macOS Apple silicon is supported as a
  client, but not as a VM host.

:::danger
Bastion APIs provide no native authentication or TLS. Tailscale Serve adds
tailnet transport security and network access control, not Bastion-level
authorization. Anyone allowed to reach the Serve URL can manage resources,
retrieve secrets, enter environments, and proxy services. Restrict access to
trusted users and devices. Do not use Tailscale Funnel for a Bastion API.

An unmodified tailnet uses a broad default allow-all policy. Joining the same
tailnet is not a sufficient restriction until you replace that policy with a
least-privilege grant. Bastion does not consume Tailscale identity headers.
:::

## Restrict the tailnet policy

1. Choose the exact operator and denied-user logins from the Tailscale admin
   console. The example below uses `operator@example.com` and
   `denied@example.com`; replace both before saving it.

2. Merge this least-privilege pattern into the tailnet policy file:

   ```json
   {
     "groups": {
       "group:bastion-operators": ["operator@example.com"]
     },
     "tagOwners": {
       "tag:bastion-host": ["autogroup:admin"]
     },
     "grants": [
       {
         "src": ["group:bastion-operators"],
         "dst": ["tag:bastion-host"],
         "ip": ["tcp:443"]
       }
     ],
     "tests": [
       {
         "src": "operator@example.com",
         "accept": ["tag:bastion-host:443"]
       },
       {
         "src": "denied@example.com",
         "deny": ["tag:bastion-host:443"]
       }
     ]
   }
   ```

   Tailscale grants are deny-by-default. Remove or narrow any existing `*` to
   `*` allow-all grant, or that broader rule will still permit access. Preserve
   unrelated groups, tags, grants, and tests that your tailnet needs. The policy
   editor must report both tests as passing before you save the change.

3. On the Bastion host, advertise the restricted tag:

   ```sh
   sudo tailscale set --advertise-tags=tag:bastion-host
   tailscale status
   ```

   Approve the tag in the admin console if your tailnet requires approval.

## Verify the Bastion host

1. On the Linux host, confirm that the services are running:

   ```sh
   sudo systemctl is-active bastiond.service bastion-api.service
   ```

2. Confirm that the API is healthy on loopback:

   ```sh
   curl -fsS http://localhost:3148/v1/health
   ```

   Expected response:

   ```json
   { "status": "ok" }
   ```

3. Keep `BASTION_ADDR` set to `localhost:3148`. You do not need to bind Bastion
   to `0.0.0.0` for Tailscale Serve.

## Publish the API

1. On the Bastion host, configure Tailscale Serve in the background:

   ```sh
   tailscale serve --https=443 --bg localhost:3148
   ```

   `--bg` keeps the Serve configuration after the command exits and across
   Tailscale restarts.

2. Display the Serve status:

   ```sh
   tailscale serve status
   ```

   The output includes an HTTPS URL such as:

   ```text
   https://bastion-host.example.ts.net
   ```

3. Set `TAILSCALE_API_URL` to the exact URL from the status output:

   ```sh
   TAILSCALE_API_URL="https://bastion-host.example.ts.net"
   ```

## Configure the remote client

1. On the client, verify tailnet connectivity:

   ```sh
   TAILSCALE_API_URL="https://bastion-host.example.ts.net"
   curl -fsS "$TAILSCALE_API_URL/v1/health"
   ```

   The response must be `{"status":"ok"}`. Define `TAILSCALE_API_URL` on the
   client if you set it only on the host.

2. From a device owned by the denied test user, verify that the same URL is
   blocked:

   ```sh
   TAILSCALE_API_URL="https://bastion-host.example.ts.net"
   if curl -fsS --connect-timeout 5 "$TAILSCALE_API_URL/v1/health"; then
     printf 'unexpected Bastion access\n' >&2
     exit 1
   else
     printf 'Bastion access blocked as expected\n'
   fi
   ```

   Do not continue if both users can connect. Recheck broad grants, device tags,
   and the policy tests.

3. On the allowed client, persist the URL for Bastion client commands:

   ```sh
   bastion client set api-url "$TAILSCALE_API_URL"
   bastion client config
   ```

   The `apiUrl.source` field should be `config`.

4. Run a remote resource command:

   ```sh
   bastion env list
   ```

5. Connect to a running remote environment:

   ```sh
   ENVIRONMENT_KEY="ENVIRONMENT_KEY"
   bastion ssh --key "$ENVIRONMENT_KEY"
   ```

   Replace `ENVIRONMENT_KEY` with a key from `bastion env list`. OpenCode and
   named service tunnels use the same configured API URL.

## Remove remote access

1. On the client, remove the persisted API URL:

   ```sh
   bastion client remove api-url
   bastion client config
   ```

   The default returns to `http://localhost:3148`.

2. On the host, inspect all Serve routes:

   ```sh
   tailscale serve status
   ```

3. Disable only the Bastion HTTPS route, using the same flags and target that
   enabled it:

   ```sh
   tailscale serve --https=443 --bg localhost:3148 off
   tailscale serve status
   ```

4. Use a global reset only if you intentionally want to remove every remaining
   Serve route:

   :::danger
   `tailscale serve reset` removes every Tailscale Serve configuration on the
   host, not only the Bastion route.
   :::

   ```sh
   tailscale serve reset
   ```

See [Connect to environments](/how-to/connect-to-environments/) and the
[host CLI reference](/reference/cli/host/) for remote client workflows. Review
[Security and operational limits](/explanation/security-and-operational-limits/)
for the API trust boundary.
