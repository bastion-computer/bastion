---
title: Expose environment services
description: Register an HTTP service tunnel and proxy it to your local machine.
---

Use a named template tunnel to reach an HTTP service bound to `localhost` inside
an environment. Bastion proxies the service over vhost-vsock through the host or
cluster API.

## Prerequisites

Before you begin:

- Build the shared base.
- Choose the guest-local HTTP port and a tunnel name.
- Ensure the client can reach the Bastion API.

Tunnel names must start with a letter and can contain letters, numbers,
underscores, and hyphens. Ports must be from `1` through `65535`.

## Register and start a service

The following reproducible example serves one static page on guest port `3000`.

1. Create `web-template.json`:

   ```sh
   cat > web-template.json <<'JSON'
   {
     "tunnels": {
       "web": 3000
     },
     "agents": {
       "opencode": {}
     },
     "actions": {
       "init": [
         {
           "run": "apt-get update && apt-get install -y --no-install-recommends ca-certificates curl python3"
         },
         {
           "run": "mkdir -p /workspace/web && printf 'hello through Bastion\\n' > /workspace/web/index.html"
         }
       ],
       "start": [
         {
           "run": "set -eu\ncd /workspace/web\nnohup python3 -m http.server 3000 --bind 127.0.0.1 > /tmp/web.log 2>&1 &\nfor i in $(seq 1 30); do curl -fsS http://127.0.0.1:3000/ >/dev/null && exit 0; sleep 1; done\ncat /tmp/web.log >&2\nexit 1"
         }
       ]
     }
   }
   JSON
   ```

   The service binds to `127.0.0.1` inside the guest. It does not need to bind
   to `0.0.0.0`; the guest proxy connects to the registered localhost port.

2. Create the template and environment:

   ```sh
   bastion templates create --key web-example --file ./web-template.json
   bastion env create --template-key web-example --key web-example-1
   ```

   The start action waits until the service responds. The environment creation
   command fails instead of reporting `running` if the service does not become
   ready.

## List tunnel URLs

1. List the environment's registered tunnels:

   ```sh
   bastion env tunnels --key web-example-1
   ```

   Expected response shape:

   ```json
   {
     "entries": [
       {
         "name": "web",
         "port": 3000,
         "url": "http://localhost:3148/v1/environments/by-key/web-example-1/tunnels/web"
       }
     ]
   }
   ```

2. For a simple endpoint, request the returned URL directly:

   ```sh
   curl -fsS http://localhost:3148/v1/environments/by-key/web-example-1/tunnels/web
   ```

   Expected output is `hello through Bastion`.

The printed URL uses your resolved `--api-url` and cluster namespace when those
are configured.

## Start a local service proxy

Use the local proxy for applications that request absolute paths such as
`/assets/app.js`.

1. Start the proxy:

   ```sh
   bastion proxy --env-key web-example-1 --name web
   ```

   Bastion prints `proxy listening on http://localhost:3000`. If that port is
   unavailable, it selects a free port and reports the fallback.

2. Open the printed URL in a browser or request it with `curl`.

3. Press `Ctrl-C` to stop the local proxy.

4. To always select a free local port, pass `--port 0` explicitly:

   ```sh
   bastion proxy --env-key web-example-1 --name web --port 0
   ```

## Listen beyond localhost

:::danger
The local proxy has no native authentication or TLS. Binding it to all
interfaces can expose the guest service to your LAN or another untrusted
network. Add appropriate firewall, authentication, and TLS controls first.
:::

1. After adding those controls, choose an explicit host and port:

   ```sh
   bastion proxy \
     --env-key web-example-1 \
     --name web \
     --host 0.0.0.0 \
     --port 8080
   ```

2. Press `Ctrl-C` when you no longer need the proxy.

Named tunnels proxy HTTP methods and paths. They are not arbitrary TCP port
forwarding.

## Change a tunnel

Templates are immutable. To add, rename, or change a tunnel port, create a
replacement template and replacement environments. Follow
[Create and manage templates](/how-to/create-manage-templates/).

## Clean up

:::caution
Removing the environment deletes its writable disk. The cleanup also deletes the
prepared template and local JSON file.
:::

1. Stop any running `bastion proxy` process with `Ctrl-C`.

2. Remove resources in dependency order:

   ```sh
   bastion env remove --key web-example-1
   bastion templates remove --key web-example
   rm ./web-template.json
   ```

See the [template configuration reference](/reference/template-configuration/),
[host CLI reference](/reference/cli/host/), and
[host API reference](/reference/api/host/) for tunnel configuration and endpoint
details. [Bastion architecture](/explanation/how-bastion-works/) explains the
guest proxy and vsock path.
