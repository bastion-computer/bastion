---
title: Deploy and operate a cluster
description: Deploy a Bastion cluster control plane, register VM hosts, and operate resources safely.
---

The cluster control plane stores source resources, schedules environments onto
registered Bastion VM hosts, and proxies client connections to the owning node.

## Prerequisites

Prepare these components before starting the control plane:

- Postgres for nodes, namespaces, secrets, templates, environments, and routing
  metadata.
- An existing S3-compatible bucket for the global base and template archives.
- One or more Linux x86_64 VM hosts with read/write KVM access and
  `/dev/vhost-vsock`.
- Private network reachability from the control plane to every node API.
- A transparently authenticated private network, such as a restricted Tailscale
  tailnet, for client and node traffic.

The cluster control plane itself can run on Linux or macOS Apple silicon. macOS
cannot act as a VM host.

:::danger
The host and cluster APIs provide no native authentication or TLS. Anyone who
can reach an API can manage resources, retrieve secrets, and enter environments.
Keep both APIs on a transparently authenticated private network. The Bastion CLI
and cluster node client cannot add arbitrary authentication headers or present a
configured client certificate. Browser SSO challenges are not compatible, and
any HTTP proxy in the path must pass upgrade traffic and long-lived streams.
:::

## Prepare each VM host

Repeat this procedure on every Linux x86_64 node.

1. Install Bastion and its VM runtime:

   ```sh
   curl -fsSL https://bastion.computer/install.sh | bash
   bastion system init --with-utilities
   bastion system check
   ```

2. Do not build a node-local base. The cluster establishes and synchronizes one
   global base after node registration.

3. Make the host API reachable only on the protected node network. In
   `/etc/default/bastion`, set:

   ```text
   BASTION_ADDR="NODE_PRIVATE_IP:3148"
   ```

   Replace `NODE_PRIVATE_IP` with the node's private address. Alternatively,
   keep Bastion on `localhost:3148` and use Tailscale Serve or a transparent TLS
   proxy on the private network. Do not require headers or client certificates
   that the cluster node client cannot supply. A proxy must support Bastion's
   upgraded SSH and tunnel streams.

4. Restart and verify the services:

   ```sh
   sudo systemctl restart bastiond.service bastion-api.service
   sudo systemctl is-active bastiond.service bastion-api.service
   ```

5. From the future control-plane host, verify each protected node URL:

   ```sh
   NODE_API_URL="https://node-a.internal.example.com"
   curl -fsS "$NODE_API_URL/v1/health"
   ```

   `NODE_API_URL` is the absolute `http` or `https` URL that the cluster will
   store. The expected response is `{"status":"ok"}`.

## Start the cluster control plane

1. Create the Postgres database and S3-compatible bucket before starting
   Bastion. Grant the control plane read, write, and delete access to the bucket.

2. On a systemd-based Linux control-plane host, create a dedicated service user
   and protected configuration directory:

   ```sh
   CLUSTER_SERVICE_USER="bastion-cluster"
   if ! id "$CLUSTER_SERVICE_USER" >/dev/null 2>&1; then
     sudo useradd --system --home-dir /var/lib/bastion-cluster \
       --create-home --shell /usr/sbin/nologin "$CLUSTER_SERVICE_USER"
   fi
   sudo install -d -o root -g "$CLUSTER_SERVICE_USER" -m 0750 /etc/bastion
   ```

   If the account already exists, skip `useradd` and verify it with
   `id "$CLUSTER_SERVICE_USER"`.

3. Create `/etc/bastion/cluster.env` with values from your database, object
   store, and secret manager:

   ```sh
   sudo install -o root -g "$CLUSTER_SERVICE_USER" -m 0640 /dev/null \
     /etc/bastion/cluster.env
   sudoedit /etc/bastion/cluster.env
   ```

   ```text
   BASTION_CLUSTER_ADDR="127.0.0.1:3150"
   BASTION_CLUSTER_DATABASE_URL="DATABASE_URL"
   BASTION_CLUSTER_S3_BUCKET="S3_BUCKET"
   BASTION_CLUSTER_S3_REGION="S3_REGION"
   BASTION_CLUSTER_S3_ACCESS_KEY_ID="S3_ACCESS_KEY_ID"
   BASTION_CLUSTER_S3_SECRET_ACCESS_KEY="S3_SECRET_ACCESS_KEY"
   BASTION_CLUSTER_LOG_FORMAT="json"
   BASTION_CLUSTER_LOG_LEVEL="warn"
   ```

   Replace `DATABASE_URL` with the Postgres connection URL. Replace `S3_BUCKET`,
   `S3_REGION`, `S3_ACCESS_KEY_ID`, and `S3_SECRET_ACCESS_KEY` with object-store
   values. For a custom S3-compatible endpoint, also add:

   ```text
   BASTION_CLUSTER_S3_ENDPOINT="S3_ENDPOINT"
   BASTION_CLUSTER_S3_USE_PATH_STYLE="true"
   ```

   Replace `S3_ENDPOINT` with the absolute endpoint URL. Enable path-style URLs
   only when your provider requires them. Install the completed file with mode
   `0640`, owned by `root` and the service group:

   ```sh
   sudo chown root:"$CLUSTER_SERVICE_USER" /etc/bastion/cluster.env
   sudo chmod 0640 /etc/bastion/cluster.env
   ```

4. Resolve the installed binary and create `bastion-cluster.service` locally:

   ```sh
   BASTION_BIN="$(command -v bastion)"
   test -x "$BASTION_BIN"
   cat > bastion-cluster.service <<EOF
   [Unit]
   Description=Bastion cluster control plane
   After=network-online.target
   Wants=network-online.target

   [Service]
   Type=simple
   User=$CLUSTER_SERVICE_USER
   Group=$CLUSTER_SERVICE_USER
   EnvironmentFile=/etc/bastion/cluster.env
   ExecStart=$BASTION_BIN start cluster
   Restart=on-failure
   RestartSec=5s

   [Install]
   WantedBy=multi-user.target
   EOF
   sudo install -o root -g root -m 0644 bastion-cluster.service \
     /etc/systemd/system/bastion-cluster.service
   rm ./bastion-cluster.service
   ```

   :::danger
   At `info` or `debug` level, the current startup record includes the full
   `BASTION_CLUSTER_DATABASE_URL`, including embedded credentials. Keep the
   managed service at `warn` or `error`, restrict journal access, and redact old
   logs. This warning also applies when you start the process in a terminal.
   :::

5. Enable and start the managed control plane:

   ```sh
   sudo systemctl daemon-reload
   sudo systemctl enable --now bastion-cluster.service
   sudo systemctl is-active bastion-cluster.service
   ```

   Postgres migrations run automatically at startup. Bastion exits if it cannot
   connect or migrate.

6. For short-lived evaluation on macOS or Linux, you can instead export the same
   settings and run `BASTION_CLUSTER_LOG_LEVEL=warn bastion start cluster` in the
   foreground. That process ends with the terminal session and is not a managed
   production deployment.

7. Verify the local control-plane process:

   ```sh
   curl -fsS http://127.0.0.1:3150/v1/health
   ```

   With no nodes registered, the expected response is `{"status":"ok"}`. The
   loopback binding should be published only through your restricted private
   network.

## Register nodes and build the base

1. Set the client URL to your private cluster endpoint:

   ```sh
   CLUSTER_API_URL="https://cluster.example.com"
   ```

   `CLUSTER_API_URL` is reachable only through your transparent private-network
   policy. The CLI cannot complete a browser SSO challenge or add custom
   authentication headers.

2. Register each node:

   ```sh
   NODE_A_API_URL="https://node-a.internal.example.com"
   bastion --api-url "$CLUSTER_API_URL" cluster nodes create \
      --key node-a \
      --url "$NODE_A_API_URL"
   ```

   With no cluster base, node registration validates only the key and URL syntax;
   it does not prove that the node is reachable. The explicit health `curl` in
   the previous procedure provides that check. If a cluster base already exists,
   registration contacts the node and imports that base before recording it.

3. List registered nodes:

   ```sh
   bastion --api-url "$CLUSTER_API_URL" cluster nodes list --limit 100
   ```

4. After at least one node exists, build the global base:

   ```sh
   bastion --api-url "$CLUSTER_API_URL" base build
   ```

   The control plane builds on one node, stores `base/base.tar.zst` in S3, and
   synchronizes it to every other registered node.

## Create and select a namespace

1. Create a namespace for one team or automation boundary:

   ```sh
   TEAM_KEY="team-a"
   bastion --api-url "$CLUSTER_API_URL" \
     cluster namespaces create --key "$TEAM_KEY"
   ```

2. Persist the protected API URL and namespace on your client:

   ```sh
   bastion client set api-url "$CLUSTER_API_URL"
   bastion client set namespace-key "$TEAM_KEY"
   bastion client config
   ```

3. Use normal resource commands in that namespace:

   ```sh
   bastion secrets list
   bastion templates list
   bastion env list
   ```

Use exactly one of `--namespace-id`, `--namespace-key`,
`BASTION_NAMESPACE_ID`, `BASTION_NAMESPACE_KEY`, or the corresponding persisted
client setting.

## Monitor the cluster

1. Check aggregate health:

   ```sh
   curl -fsS "$CLUSTER_API_URL/v1/health"
   ```

   Health fails when any registered node is unreachable or unhealthy.

2. Check aggregate capacity:

   ```sh
   bastion --api-url "$CLUSTER_API_URL" utilization
   ```

3. Check resources in a namespace:

   ```sh
   bastion --api-url "$CLUSTER_API_URL" \
     --namespace-key "$TEAM_KEY" \
     env list --limit 100
   ```

Environment creation checks declared vCPU, memory, and volume requirements
against node capacity. It fails rather than intentionally overcommitting a node.
Cluster operations span Postgres, S3, and node APIs without a distributed
transaction. Bastion attempts compensating cleanup after failures, but a network
failure can leave node-local derivatives or archive objects that require manual
inventory and reconciliation before you retry.

## Take a node out of service

:::danger
Bastion has no cordon, node drain, live migration, rescheduling, or failover.
Removing a node does not move or preserve its environments. Track node assignment
in an external operational inventory because source environment responses do not
expose the selected node.
:::

1. Schedule a maintenance window and halt user and automation requests that can
   create environments. Bastion cannot cordon the node while other cluster
   operations continue.

2. Use your external node-assignment inventory to identify the environments on
   the node, then preserve all work from them.

3. While the original node API remains reachable, remove each affected source
   environment through its namespace. Normal removal first calls that node API
   to delete the derivative VM, then removes the source record:

   ```sh
   TEAM_KEY="TEAM_KEY"
   ENVIRONMENT_KEY="ENVIRONMENT_KEY"
   bastion --api-url "$CLUSTER_API_URL" \
     --namespace-key "$TEAM_KEY" \
     env remove --key "$ENVIRONMENT_KEY"
   ```

   Replace `TEAM_KEY` and `ENVIRONMENT_KEY` with the affected resource keys.

4. After all source environments assigned to the node are gone, remove it from
   the scheduler:

   ```sh
   NODE_KEY="NODE_KEY"
   bastion --api-url "$CLUSTER_API_URL" \
     cluster nodes remove --key "$NODE_KEY"
   ```

   Replace `NODE_KEY` with the node key. Postgres rejects node removal while a
   source environment still references it.

5. Complete node maintenance or replacement, then resume environment creation.
   If you create replacements, they are new clean environments; this procedure
   does not drain or recover the removed writable disks.

## Recover a failed node

If a node becomes unreachable, source environments remain assigned to its stored
URL. There is no automatic failover or partial reassignment.

1. Restore the original node API at the same stored URL whenever possible.

2. After it is reachable, preserve work and follow the planned removal procedure
   above.

3. If the node is permanently lost, normal `env remove` fails because it must
   call the original node API before deleting the source row. The source rows
   then prevent normal `cluster nodes remove` through Postgres references.
   Bastion has no supported CLI force-detach, lost-node recovery, or force node
   removal workflow. Registering a replacement node does not take ownership of
   those environments.

4. Do not delete the namespace to bypass the failure; that can leave derivative
   resources and S3 objects without supported cleanup. Restore the node or use a
   separately reviewed, manual database-recovery procedure with full Postgres,
   S3, and node inventory. Direct database repair is outside the supported CLI
   workflow.

## Remove a namespace safely

:::danger
Namespace deletion is not recursive resource cleanup. Database cascades can
remove source rows without calling node teardown or deleting template objects
from S3. This can leave running VMs and orphaned archives. Never use namespace
removal as a shortcut for resource cleanup.
:::

1. Select the namespace and list its resources.

2. Preserve work, then remove every environment.

3. Remove every template after its environments are gone.

4. Remove every secret after its template consumers are gone.

5. Confirm all three lists are empty, then remove the namespace:

   ```sh
   TEAM_KEY="TEAM_KEY"
   bastion --api-url "$CLUSTER_API_URL" \
     cluster namespaces remove --key "$TEAM_KEY"
   ```

6. Remove persisted namespace selection if it pointed to the deleted namespace:

   ```sh
   bastion client remove namespace-key
   ```

Back up Postgres and S3 outside Bastion. See
[Back up and restore Bastion artifacts](/how-to/back-up-and-restore/), the
[host requirements and configuration](/reference/host-requirements-and-configuration/),
and the [cluster CLI reference](/reference/cli/cluster/). See
[Clusters and namespaces](/explanation/clusters-and-namespaces/) for placement,
derivative, and partial-operation behavior.
