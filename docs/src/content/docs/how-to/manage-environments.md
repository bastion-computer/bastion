---
title: Manage environments
description: Create, find, inspect, size, and remove Bastion VM environments.
---

Use environment keys for human-operated workflows and generated IDs for
automation. An environment is a writable VM overlay backed by one immutable
template.

## Prerequisites

Before you create an environment:

- Ensure the host API and daemon are healthy.
- Build or import the exact base recorded by the template.
- Create the template and any secrets it resolves during environment startup.
- Check capacity with `bastion utilization`.

## Create an environment

1. Create an environment from a keyed template, assign a unique environment key,
   and add repeatable tags:

   ```sh
   bastion env create \
     --template-key project \
     --key issue-123 \
     --tag repo:project \
     --tag task:issue-123
   ```

   Bastion creates a fresh writable overlay, cold-boots the VM, refreshes agent
   configuration, runs `actions.start`, and waits for readiness. The final JSON
   must have `status` set to `running`.

2. If the template has no key, create from its generated ID instead:

   ```sh
   TEMPLATE_ID="TEMPLATE_ID"
   bastion env create --template-id "$TEMPLATE_ID" --key issue-124
   ```

   Replace `TEMPLATE_ID` with the template's generated `tpl_` ID. Specify
   exactly one of `--template-key` or `--template-id`.

## List and filter environments

1. List the first page:

   ```sh
   bastion env list --limit 100
   ```

2. Filter by one or more tags:

   ```sh
   bastion env list --tag repo:project --tag task:issue-123
   ```

   Repeated tag filters use `AND`. An entry must contain every supplied tag.

3. If the response has a non-null `cursor`, request the next page:

   ```sh
   CURSOR="CURSOR"
   bastion env list --limit 100 --cursor "$CURSOR"
   ```

   Replace `CURSOR` with the previous response's cursor.

## Inspect status and capacity

1. Get one environment by key:

   ```sh
   bastion env get --key issue-123
   ```

   `get` reconciles stored metadata with the daemon before returning it. If
   status is `error`, inspect `lastError`.

2. Inspect host or cluster capacity:

   ```sh
   bastion utilization
   ```

   `memory` and `volume` values are bytes. Capacity is counted for VMs whose
   persisted or live state is `creating`, `running`, or `paused`.

3. Use these status meanings when deciding what to do:

   | Status     | Action                                                               |
   | ---------- | -------------------------------------------------------------------- |
   | `creating` | Wait for the create command or inspect streamed errors.              |
   | `running`  | Connect, proxy services, or continue work.                           |
   | `paused`   | Inspect daemon state; the CLI has no pause or resume command.        |
   | `stopped`  | Remove and recreate the environment; the CLI has no restart command. |
   | `error`    | Read `lastError`, fix the cause, then remove and recreate.           |

## Remove an environment

:::danger
Environment removal stops the VM and permanently deletes its writable disk.
Export, commit, or push work before you continue. Bastion template archives do
not include environment writable disks.
:::

1. Confirm that you selected the intended environment:

   ```sh
   bastion env get --key issue-123
   ```

2. Remove it:

   ```sh
   bastion env remove --key issue-123
   ```

   For an unkeyed environment, set `ENVIRONMENT_ID="ENVIRONMENT_ID"` to its
   generated `env_` ID and use `--id "$ENVIRONMENT_ID"`.

3. Confirm it no longer appears:

   ```sh
   bastion env list --tag task:issue-123
   ```

4. Remove a template only after all environment records that reference it are
   gone.

## Manage cluster environments

Persist a cluster API URL and one namespace when you use the same scope often:

```sh
CLUSTER_API_URL="https://cluster.example.com"
TEAM_KEY="TEAM_KEY"
bastion client set api-url "$CLUSTER_API_URL"
bastion client set namespace-key "$TEAM_KEY"
bastion client config
```

Replace `TEAM_KEY` with the namespace key. The normal `env` commands then use
that namespace.

Cluster scheduling selects a registered node with sufficient declared capacity.
It does not provide cordon, node drain, live migration, rescheduling, or
failover. If a node fails, its environments remain assigned to the original node
URL. Normal `env remove` must reach that node API before it deletes the source
record. Recover the original node at the same URL before removal. If it is
permanently lost, there is no supported CLI force-detach or recovery workflow,
and registering a replacement node does not take over its environments.

See [Connect to environments](/how-to/connect-to-environments/),
[Expose environment services](/how-to/expose-environment-services/), and the
[host CLI reference](/reference/cli/host/) for related workflows. See
[Environment states and streams](/reference/environment-states-and-streams/)
for exact status and reconciliation behavior, and
[Clusters and namespaces](/explanation/clusters-and-namespaces/) for cluster
placement limits.
