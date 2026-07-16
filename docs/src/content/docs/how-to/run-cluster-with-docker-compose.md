---
title: Run a cluster with Docker Compose
description: Run a local Bastion cluster control plane with Postgres and MinIO.
---

Use this Docker Compose stack for local cluster control-plane evaluation. It
starts the published Bastion image, Postgres 18, and MinIO on one machine. It
does not start a VM host; register separate prepared Linux x86_64 nodes before
creating templates or environments.

## Prerequisites

Install these local tools:

- Docker Engine with the Compose plugin.
- `curl`.
- `openssl` for generating local credentials.

Confirm Docker Compose:

```sh
docker compose version
```

## Create local credentials

:::caution
This procedure stores generated database and object-storage credentials in a
local `.env` file. Keep its mode at `600`, do not commit it, and use a proper
secret manager for a production deployment. Compose also places resolved values
in container configuration. Anyone with Docker daemon access can retrieve them
with inspection commands or the Docker API.
:::

1. Create and enter a working directory:

   ```sh
   mkdir bastion-cluster-compose
   cd bastion-cluster-compose
   ```

2. Generate credentials and write `.env` with restricted permissions:

   ```sh
   umask 077
   POSTGRES_PASSWORD="$(openssl rand -hex 24)"
   MINIO_ROOT_PASSWORD="$(openssl rand -hex 24)"
   printf '%s\n' \
     'POSTGRES_VERSION=18.0' \
     'MINIO_VERSION=RELEASE.2025-09-07T16-13-09Z' \
     'MINIO_MC_VERSION=RELEASE.2025-08-13T08-35-41Z' \
     'BASTION_VERSION=v0.27.0' \
     "POSTGRES_PASSWORD=$POSTGRES_PASSWORD" \
     'MINIO_ROOT_USER=bastionadmin' \
     "MINIO_ROOT_PASSWORD=$MINIO_ROOT_PASSWORD" \
     > .env
   unset POSTGRES_PASSWORD MINIO_ROOT_PASSWORD
   chmod 0600 .env
   ```

   These explicit versions make the example reproducible. Review release notes
   and update each value deliberately rather than switching to a mutable
   `latest` tag.

## Create the Compose stack

1. Create `compose.yml`:

   ```sh
   cat > compose.yml <<'YAML'
   services:
     postgres:
       image: postgres:${POSTGRES_VERSION:?set POSTGRES_VERSION in .env}
       restart: unless-stopped
       environment:
         POSTGRES_USER: bastion
         POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:?set POSTGRES_PASSWORD in .env}
         POSTGRES_DB: bastion_cluster
       healthcheck:
         test: ["CMD-SHELL", "pg_isready -U bastion -d bastion_cluster"]
         interval: 2s
         timeout: 5s
         retries: 30
       volumes:
         - postgres-data:/var/lib/postgresql

     minio:
       image: minio/minio:${MINIO_VERSION:?set MINIO_VERSION in .env}
       restart: unless-stopped
       command: ["server", "/data", "--console-address", ":9001"]
       environment:
         MINIO_ROOT_USER: ${MINIO_ROOT_USER:?set MINIO_ROOT_USER in .env}
         MINIO_ROOT_PASSWORD: ${MINIO_ROOT_PASSWORD:?set MINIO_ROOT_PASSWORD in .env}
       ports:
         - "127.0.0.1:9001:9001"
       volumes:
         - minio-data:/data

     minio-create-bucket:
       image: minio/mc:${MINIO_MC_VERSION:?set MINIO_MC_VERSION in .env}
       depends_on:
         minio:
           condition: service_started
       environment:
         MINIO_ROOT_USER: ${MINIO_ROOT_USER:?set MINIO_ROOT_USER in .env}
         MINIO_ROOT_PASSWORD: ${MINIO_ROOT_PASSWORD:?set MINIO_ROOT_PASSWORD in .env}
       entrypoint: ["/bin/sh", "-c"]
       command:
         - |
           until mc alias set local http://minio:9000 "$$MINIO_ROOT_USER" "$$MINIO_ROOT_PASSWORD" >/dev/null 2>&1; do
             sleep 1
           done
           mc mb --ignore-existing local/bastion-cluster

     cluster-api:
       image: bastioncomputer/bastion:${BASTION_VERSION:?set BASTION_VERSION in .env}
       restart: unless-stopped
       command: ["start", "cluster"]
       depends_on:
         postgres:
           condition: service_healthy
         minio-create-bucket:
           condition: service_completed_successfully
       environment:
         BASTION_CLUSTER_ADDR: ":3150"
         BASTION_CLUSTER_DATABASE_URL: postgres://bastion:${POSTGRES_PASSWORD}@postgres:5432/bastion_cluster?sslmode=disable
         BASTION_CLUSTER_S3_BUCKET: bastion-cluster
         BASTION_CLUSTER_S3_ENDPOINT: http://minio:9000
         BASTION_CLUSTER_S3_REGION: us-east-1
         BASTION_CLUSTER_S3_ACCESS_KEY_ID: ${MINIO_ROOT_USER}
         BASTION_CLUSTER_S3_SECRET_ACCESS_KEY: ${MINIO_ROOT_PASSWORD}
         BASTION_CLUSTER_S3_USE_PATH_STYLE: "true"
         BASTION_CLUSTER_LOG_FORMAT: text
         BASTION_CLUSTER_LOG_LEVEL: warn
       ports:
         - "127.0.0.1:3150:3150"

   volumes:
     postgres-data:
     minio-data:
   YAML
   ```

   The API and MinIO console bind only to host loopback. Postgres and the MinIO
   S3 endpoint remain on the Compose network. The cluster process uses `warn`
   because its current `info` startup record includes the complete database URL,
   including the generated password.

2. Validate without printing the resolved secrets:

   ```sh
   docker compose config --quiet
   ```

## Start and verify the control plane

:::danger
The Bastion cluster API provides no native authentication or TLS. This example
binds it to `127.0.0.1` for local evaluation. Do not change the published address
to an untrusted interface. For remote clients, prefer a transparently
authenticated private network such as Tailscale. The CLI cannot add arbitrary
authentication headers or present a configured client certificate, and any HTTP
proxy must pass upgrade traffic and long-lived streams.
:::

1. Start the stack:

   ```sh
   docker compose up -d
   ```

2. Wait for the cluster API:

   ```sh
   until curl -fsS http://localhost:3150/v1/health; do sleep 1; done
   ```

   With no registered nodes, the response is `{"status":"ok"}`.

3. Inspect control-plane logs when needed:

   ```sh
   docker compose logs cluster-api
   ```

   Keep the log level at `warn` or `error`. If you change it to `info` or
   `debug`, the startup log emits the full database URL. Restrict log and Docker
   daemon access and redact credentials before sharing output.

4. Open the MinIO console at `http://localhost:9001`. Read the generated
   `MINIO_ROOT_USER` and `MINIO_ROOT_PASSWORD` from `.env` when you need to sign
   in; do not paste them into logs. These values are also present in Docker's
   inspectable container configuration, so `.env` is not their only copy.

## Smoke test namespace storage

1. Create an empty namespace through the CLI in the cluster container:

   ```sh
   docker compose exec -T cluster-api \
     bastion --api-url http://localhost:3150 \
     cluster namespaces create --key compose-check
   ```

2. List namespaces:

   ```sh
   docker compose exec -T cluster-api \
     bastion --api-url http://localhost:3150 \
     cluster namespaces list
   ```

3. Remove the empty smoke-test namespace:

   ```sh
   docker compose exec -T cluster-api \
     bastion --api-url http://localhost:3150 \
     cluster namespaces remove --key compose-check
   ```

Namespace deletion is safe here only because `compose-check` is empty. It is not
a recursive cleanup command for populated namespaces.

## Add VM hosts

Prepare and secure external Bastion nodes by following
[Deploy and operate a cluster](/how-to/deploy-and-operate-cluster/). Each node
URL must be reachable from the `cluster-api` container. After registration,
build the global base through `http://localhost:3150` before creating templates.

The Compose stack only supplies the control-plane dependencies. It does not add
KVM devices or run `bastion start daemon`.

## Stop or delete the stack

1. Stop containers while retaining Postgres and MinIO volumes:

   ```sh
   docker compose down
   ```

2. To restart with the retained state:

   ```sh
   docker compose up -d
   ```

3. Only when you no longer need any control-plane data or archives, delete the
   volumes:

   :::danger
   `docker compose down -v` permanently deletes this stack's Postgres database
   and MinIO objects. It does not remove derivative resources from registered
   VM hosts. Clean up environments, templates, secrets, namespaces, and nodes
   through Bastion first.
   :::

   ```sh
   docker compose down -v
   rm -f .env compose.yml
   ```

See [Host requirements and configuration](/reference/host-requirements-and-configuration/)
and the [cluster CLI reference](/reference/cli/cluster/) for control-plane
settings. [Clusters and namespaces](/explanation/clusters-and-namespaces/)
explains why the Compose volumes are only part of cluster state.
