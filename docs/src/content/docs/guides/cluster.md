---
title: Cluster
description: Run Bastion across multiple Linux nodes with the cluster control plane.
---

The Bastion cluster control plane lets one public API manage many Linux machines
running the normal Bastion host API and daemon. Clients keep using the same
`secrets`, `templates`, and `env` commands, while the control plane stores source
resources, schedules environments onto registered nodes, and proxies SSH,
OpenCode, and tunnel traffic to the node that owns each environment.

Use a cluster when a single Bastion host is not enough. Each node still runs its
own `bastion start daemon` and `bastion start api`; the cluster API sits in front
of those nodes and coordinates shared state.

## Requirements

The cluster control plane needs:

| Requirement               | Purpose                                                                 |
| ------------------------- | ----------------------------------------------------------------------- |
| Postgres database         | Stores cluster nodes, namespaces, source resources, and routing state.  |
| S3-compatible bucket      | Stores prepared source template archives shared across nodes.           |
| One or more Bastion nodes | Run the standard Bastion host API and daemon on Linux with KVM support. |

Every node URL you register must be reachable from the cluster control plane.
Clients only need to reach the cluster API unless they also manage nodes
directly.

## Start Bastion Nodes

On each Linux machine that will run environments, install Bastion and prepare the
Cloud Hypervisor runtime as usual:

```sh
bastion system add cloud-hypervisor --with-utilities
sudo bastion start daemon
bastion start api --addr 0.0.0.0:3148
```

Expose the host API at an HTTP or HTTPS URL reachable by the control plane, such
as `https://node-a.internal:3148`. The node API remains the same single-host API
documented elsewhere; cluster-managed resources on the node are derivatives that
the control plane creates and cleans up.

## Start the Cluster API

Start the control plane with a Postgres URL and S3 archive storage:

```sh
bastion start cluster \
  --addr 0.0.0.0:3150 \
  --database-url postgres://bastion:password@postgres.internal:5432/bastion_cluster?sslmode=require \
  --s3-bucket bastion-cluster-templates \
  --s3-region us-east-1
```

For S3-compatible services such as MinIO, also pass the endpoint and path-style
flag:

```sh
bastion start cluster \
  --database-url postgres://bastion:bastion@localhost:3151/bastion_cluster?sslmode=disable \
  --s3-bucket bastion-cluster \
  --s3-endpoint http://127.0.0.1:9000 \
  --s3-region us-east-1 \
  --s3-access-key-id minioadmin \
  --s3-secret-access-key minioadmin \
  --s3-use-path-style
```

The cluster API listens on `localhost:3150` by default. In production, run it
behind your normal service manager and TLS boundary.

## Register Nodes

Point the CLI at the cluster API and add each Bastion node:

```sh
bastion --api-url http://cluster.internal:3150 cluster nodes create \
  --key node-a \
  --url https://node-a.internal:3148

bastion --api-url http://cluster.internal:3150 cluster nodes create \
  --key node-b \
  --url https://node-b.internal:3148
```

Inspect and remove nodes with:

```sh
bastion --api-url http://cluster.internal:3150 cluster nodes list
bastion --api-url http://cluster.internal:3150 cluster nodes get --key node-a
bastion --api-url http://cluster.internal:3150 cluster nodes remove --key node-b
```

`bastion cluster` commands default to `http://localhost:3150` when no
`--api-url`, `BASTION_API_URL`, or client config is set. Other resource commands
keep the normal host API default of `http://localhost:3148`, so pass `--api-url`
or persist it before managing cluster resources.

## Create Namespaces

Cluster resource commands require a namespace. Namespaces isolate source
secrets, templates, environment keys, and environment lists.

Create one namespace per tenant, team, or automation context:

```sh
bastion --api-url http://cluster.internal:3150 cluster namespaces create --key team-a
bastion --api-url http://cluster.internal:3150 cluster namespaces list
```

Use either the namespace ID or key on resource commands:

```sh
bastion --api-url http://cluster.internal:3150 \
  --namespace-key team-a \
  secrets create --key OPENAI_API_KEY --value "$OPENAI_API_KEY"
```

The CLI keeps namespace selection in flags, environment variables, or persisted
client config, but cluster API requests encode the namespace in the path, such as
`/v1/namespaces/ns_xxxxxx/templates` or
`/v1/namespaces/by-key/team-a/templates`.

For regular use, persist the cluster API URL and namespace locally:

```sh
bastion client set api-url http://cluster.internal:3150
bastion client set namespace-key team-a
bastion client config
```

After that, normal resource commands target the cluster namespace:

```sh
bastion secrets list
bastion templates list
bastion env list
```

## Manage Resources

With `--api-url` pointed at the cluster API and a namespace selected, use the same
commands as a single-node Bastion host:

```sh
bastion templates create --key dev --file ./template.json
bastion env create --template-key dev --key review-123 --tag repo:bastion
bastion env tunnels --key review-123
bastion ssh --key review-123
bastion proxy --env-key review-123 --name frontend
bastion opencode --key review-123
```

The control plane stores the source secret, template, and environment IDs in
Postgres. It creates node-local derivative resources only when needed. Template
archives are stored in the configured S3 bucket so any node can restore the
prepared source template before launching an environment.

During `templates create` and `env create`, the cluster API streams control-plane
progress such as node selection, derivative import/export, archive storage, and
record persistence through the normal `log` events. The CLI prints these
`cluster:` progress lines to stderr alongside node-level creation logs.

Environment creation chooses a node with enough available vCPU, memory, and
volume for the template's declared resources. If no registered node has enough
capacity, `bastion env create` fails instead of overcommitting the cluster.

## Operations

Use aggregate health and utilization through the cluster API:

```sh
bastion --api-url http://cluster.internal:3150 utilization
curl -fsS http://cluster.internal:3150/v1/health
```

`/v1/utilization` sums capacity across registered nodes. Health checks fail if a
registered node is not reachable or does not report healthy.

Removing a cluster environment removes the derivative environment from its node.
When no environments use a derivative template on a node, the control plane also
removes that derivative template and its derivative secrets.

Removing a source template is blocked while environments still use it. Remove the
environments first, then remove the template and its stored archive.
