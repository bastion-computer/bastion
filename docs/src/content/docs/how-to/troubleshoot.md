---
title: Troubleshoot Bastion
description: Diagnose client, service, VM, network, base, and cluster failures.
---

Start with the smallest failing layer: client configuration, API process,
daemon socket, host dependencies, VM runtime, then cluster dependencies.

## Run baseline checks

1. Record the binary and resolved client configuration:

   ```sh
   bastion version
   bastion client config
   ```

   Verify `apiUrl.value`, `apiUrl.source`, and any cluster namespace selector.
   An environment variable or persisted client setting can silently direct a
   command to another API.

2. On a Linux VM host, check runtime dependencies:

   ```sh
   bastion system check
   ```

3. Check the installed services and local health endpoint:

   ```sh
   sudo systemctl is-active bastiond.service bastion-api.service
   curl -fsS http://localhost:3148/v1/health
   ```

4. Check current resources and capacity:

   ```sh
   bastion base get
   bastion templates list --limit 100
   bastion env list --limit 100
   bastion utilization
   ```

## Fix client connection failures

1. If the CLI reports connection refused or targets the wrong host, inspect the
   resolved source:

   ```sh
   bastion client config
   ```

2. Test the intended API explicitly:

   ```sh
   API_URL="http://localhost:3148"
   curl -fsS "$API_URL/v1/health"
   bastion --api-url "$API_URL" templates list
   ```

   `API_URL` is the exact host or cluster API URL you intend to use.

3. Remove a stale persisted override:

   ```sh
   bastion client remove api-url
   ```

4. If `BASTION_API_URL` is set, unset it or correct it because environment
   values take precedence over persisted client configuration:

   ```sh
   unset BASTION_API_URL
   ```

5. For cluster resources, select exactly one namespace. Clear conflicting
   values before setting the intended key:

   ```sh
   unset BASTION_NAMESPACE_ID BASTION_NAMESPACE_KEY
   TEAM_KEY="TEAM_KEY"
   bastion client set namespace-key "$TEAM_KEY"
   ```

   Replace `TEAM_KEY` with an existing namespace key.

## Inspect service startup failures

1. Read recent logs from both installed services:

   ```sh
   sudo journalctl \
     -u bastiond.service \
     -u bastion-api.service \
     --since '15 minutes ago' \
     --no-pager
   ```

2. Inspect the shared configuration:

   ```sh
   sudo cat /etc/default/bastion
   sudo systemctl cat bastiond.service bastion-api.service
   ```

   Both services must use the same `BASTION_DATA_DIR` and compatible daemon
   socket paths.

3. Restart the daemon before the API, then check status:

   ```sh
   sudo systemctl restart bastiond.service
   sudo systemctl restart bastion-api.service
   sudo systemctl status bastiond.service bastion-api.service --no-pager
   ```

The API logs structured request fields including `request_id`, route, status,
duration, and error. Responses include `X-Request-ID`, which you can correlate
with service logs.

## Fix missing host dependencies

1. Read every failed `[x]` leaf from:

   ```sh
   bastion system check
   ```

2. Confirm the VM-host requirements directly:

   ```sh
   uname -m
   test -r /dev/kvm && test -w /dev/kvm
   test -e /dev/vhost-vsock
   ```

   VM hosting requires Linux x86_64, read/write KVM, and vhost-vsock. macOS
   Apple silicon supports the client and cluster control plane only.

3. Install missing supported utilities and pinned assets:

   :::caution
   The next command can install operating-system packages through `sudo` and
   downloads runtime assets.
   :::

   ```sh
   bastion system init --with-utilities
   bastion system check
   ```

4. If the host is itself a VM and `/dev/kvm` is unavailable, enable nested
   virtualization or move Bastion to a compatible instance type.

## Fix daemon socket permission errors

SSH, OpenCode, tunnels, and VM operations fail if the API service user cannot
open the daemon or per-VM proxy sockets.

1. Inspect the socket owner and mode:

   ```sh
   sudo stat -c '%U:%G %a %n' /run/bastion/bastiond.sock
   sudo systemctl show bastion-api.service --property=User,Group
   ```

2. If you installed with the official installer, rerun it to restore current
   unit definitions while preserving `/etc/default/bastion`:

   ```sh
   curl -fsSL https://bastion.computer/install.sh | bash
   ```

3. For manual services, start the daemon with `--socket-uid` and `--socket-gid`
   set to the API service user's numeric UID and GID. The same owner is applied
   to per-VM proxy sockets and the base SSH key.

## Inspect failed template or environment creation

1. Read streamed stderr from the failing `templates create` or `env create`
   command. Init and start action output is sent there.

2. Find a persisted failed environment and read `lastError`:

   ```sh
   bastion env list --limit 100
   ENVIRONMENT_KEY="ENVIRONMENT_KEY"
   bastion env get --key "$ENVIRONMENT_KEY"
   ```

   Replace `ENVIRONMENT_KEY` with the failed environment key.

3. Determine the configured data directory from `/etc/default/bastion`, then
   inspect any retained VM logs:

   ```sh
   BASTION_DATA_DIR="/home/YOUR_USER/.bastion"
   ENVIRONMENT_ID="ENVIRONMENT_ID_VALUE"
   sudo ls -la "$BASTION_DATA_DIR/environments/$ENVIRONMENT_ID"
   sudo cat "$BASTION_DATA_DIR/environments/$ENVIRONMENT_ID/stderr.log"
   sudo cat "$BASTION_DATA_DIR/environments/$ENVIRONMENT_ID/serial.log"
   sudo cat "$BASTION_DATA_DIR/environments/$ENVIRONMENT_ID/dnsmasq.log"
   ```

   Replace `YOUR_USER` with the service account name and
   `ENVIRONMENT_ID_VALUE` with the exact generated `env_` ID from the API
   response. Depending on the failure phase, Bastion can remove the environment
   directory, so some logs might not exist.

4. After fixing the cause, remove the failed record and create a fresh
   environment. Bastion has no restart command:

   :::caution
   Removal deletes any retained writable disk.
   :::

   ```sh
   TEMPLATE_KEY="TEMPLATE_KEY"
   bastion env remove --key "$ENVIRONMENT_KEY"
   bastion env create --template-key "$TEMPLATE_KEY" --key "$ENVIRONMENT_KEY"
   ```

   Replace `TEMPLATE_KEY` with the source template key.

## Fix base content-address errors

1. Compare the current base and template metadata:

   ```sh
   bastion base get
   TEMPLATE_KEY="TEMPLATE_KEY"
   bastion templates get --key "$TEMPLATE_KEY"
   ```

2. If `contentAddress` differs from `baseContentAddress`, either restore the
   exact matching base or recreate the template against the current base.

3. Do not use a force replacement as a generic repair:

   :::danger
   Force-building or force-importing a base can make every template prepared
   from the previous base unusable. In a cluster, it affects every namespace.
   :::

   Follow [Manage the base](/how-to/manage-base/) for safe replacement.

## Fix VM network conflicts

1. Inspect the host's routes:

   ```sh
   ip -4 route
   ```

2. If Bastion's default `10.241.0.0/16` overlaps another route, choose an unused
   two-octet prefix such as `10.242`.

3. When no Bastion environments are running, set the prefix in
   `/etc/default/bastion`:

   ```text
   BASTION_VM_NETWORK_PREFIX="10.242"
   ```

4. Restart both services and create a fresh environment:

   ```sh
   sudo systemctl restart bastiond.service bastion-api.service
   ```

Changing the prefix does not migrate or fail over existing environments.

## Diagnose cluster failures

1. Verify the cluster API and list nodes explicitly:

   ```sh
   CLUSTER_API_URL="https://cluster.example.com"
   curl -fsS "$CLUSTER_API_URL/v1/health"
   bastion --api-url "$CLUSTER_API_URL" cluster nodes list --limit 100
   ```

2. From the control-plane host, request `/v1/health` and `/v1/utilization` on
   every stored node URL. Aggregate health and utilization fail when any node is
   unreachable.

3. If startup fails, verify Postgres connectivity and the
   `BASTION_CLUSTER_DATABASE_URL` value. Migrations run automatically and a
   migration error prevents startup. The current `info` startup record logs the
   full database URL, including embedded credentials. Use `warn` or `error` for
   `BASTION_CLUSTER_LOG_LEVEL`, restrict log access, and redact old logs before
   sharing them.

4. If base, template, or environment orchestration reports that archive storage
   is not configured, set `BASTION_CLUSTER_S3_BUCKET` and the matching region,
   endpoint, credentials, and path-style option, then restart the cluster API.

5. Verify that the bucket exists and the control plane can get, put, and delete
   objects. Cluster base and template operations require S3-compatible storage.

6. Do not expect another node to take over a failed environment. Bastion has no
   cordon, node drain, rescheduling, or failover. Normal environment removal
   calls the original node API before deleting its source record. Recover the
   node at its stored URL, then preserve work and remove the environment. If the
   node is permanently lost, there is no supported CLI force-detach, force node
   removal, or environment recovery workflow.

7. Reconcile failed create or remove operations across Postgres, S3, and the
   affected node before retrying. These operations are not distributed
   transactions. Bastion attempts cleanup, but a network failure can leave
   source records, node-local derivatives, or archive objects in partial state.

## Gather useful diagnostics

Before reporting a problem, collect:

```sh
bastion version
bastion client config
bastion system check
bastion utilization
sudo systemctl status bastiond.service bastion-api.service --no-pager
sudo journalctl -u bastiond.service -u bastion-api.service --since '15 minutes ago' --no-pager
```

Remove secret values, database URLs, credentials, private keys, and sensitive
guest output before sharing logs. See the
[host requirements and configuration](/reference/host-requirements-and-configuration/),
[host CLI reference](/reference/cli/host/),
[cluster CLI reference](/reference/cli/cluster/), and
[environment states and streams](/reference/environment-states-and-streams/)
for defaults and response semantics. Review
[Security and operational limits](/explanation/security-and-operational-limits/)
before sharing or exposing diagnostics.
