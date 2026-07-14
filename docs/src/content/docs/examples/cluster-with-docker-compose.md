---
title: Cluster with Docker Compose
description: Run the Bastion Cluster API with Postgres 18 and MinIO using Docker Compose.
---

Use Docker Compose to run the Bastion cluster control plane, a Postgres 18
database, and MinIO for S3-compatible base and template archive storage on one
machine.
The Bastion API container uses the latest published Docker Hub image:
`bastioncomputer/bastion:latest`.

This example starts the cluster API only. To create environments, register one or
more Bastion host API nodes that are reachable from the cluster API container.

## Requirements

You need Docker with the Compose plugin installed:

```sh
docker compose version
```

## Create the Compose File

Create a working directory and add this `compose.yml`:

```yaml
services:
  postgres:
    image: postgres:18
    environment:
      POSTGRES_USER: bastion
      POSTGRES_PASSWORD: bastion
      POSTGRES_DB: bastion_cluster
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U bastion -d bastion_cluster"]
      interval: 2s
      timeout: 5s
      retries: 30
    volumes:
      - postgres-data:/var/lib/postgresql

  minio:
    image: minio/minio:latest
    command: ["server", "/data", "--console-address", ":9001"]
    environment:
      MINIO_ROOT_USER: minioadmin
      MINIO_ROOT_PASSWORD: minioadmin
    ports:
      - "9000:9000"
      - "9001:9001"
    volumes:
      - minio-data:/data

  minio-create-bucket:
    image: minio/mc:latest
    depends_on:
      minio:
        condition: service_started
    entrypoint: ["/bin/sh", "-c"]
    command:
      - |
        until mc alias set local http://minio:9000 minioadmin minioadmin >/dev/null 2>&1; do
          sleep 1
        done
        until mc mb --ignore-existing local/bastion-cluster; do
          sleep 1
        done

  cluster-api:
    image: bastioncomputer/bastion:latest
    command: ["start", "cluster"]
    depends_on:
      postgres:
        condition: service_healthy
      minio-create-bucket:
        condition: service_completed_successfully
    environment:
      BASTION_CLUSTER_ADDR: ":3150"
      BASTION_CLUSTER_DATABASE_URL: postgres://bastion:bastion@postgres:5432/bastion_cluster?sslmode=disable
      BASTION_CLUSTER_S3_BUCKET: bastion-cluster
      BASTION_CLUSTER_S3_ENDPOINT: http://minio:9000
      BASTION_CLUSTER_S3_REGION: us-east-1
      BASTION_CLUSTER_S3_ACCESS_KEY_ID: minioadmin
      BASTION_CLUSTER_S3_SECRET_ACCESS_KEY: minioadmin
      BASTION_CLUSTER_S3_USE_PATH_STYLE: "true"
      BASTION_CLUSTER_LOG_FORMAT: text
      BASTION_CLUSTER_LOG_LEVEL: info
    ports:
      - "3150:3150"

volumes:
  postgres-data:
  minio-data:
```

Postgres and MinIO data are stored in Docker volumes. The `minio-create-bucket`
service waits for MinIO, creates the `bastion-cluster` bucket, and exits before
the cluster API starts.

## Start the Cluster API

Validate the Compose file:

```sh
docker compose config
```

Start the services:

```sh
docker compose up -d
```

Wait for the cluster API health check to return `ok`:

```sh
curl -fsS http://localhost:3150/v1/health
```

The response should be:

```json
{ "status": "ok" }
```

View cluster API logs with:

```sh
docker compose logs cluster-api
```

Add `-f` to follow new log output.

The MinIO console is available at `http://localhost:9001` with username
`minioadmin` and password `minioadmin`.

## Smoke Test the Cluster API

Run the Bastion CLI from the cluster API container:

```sh
docker compose exec -T cluster-api \
  bastion --api-url http://localhost:3150 cluster namespaces create --key compose-demo
```

List namespaces:

```sh
docker compose exec -T cluster-api \
  bastion --api-url http://localhost:3150 cluster namespaces list
```

Remove the smoke-test namespace:

```sh
docker compose exec -T cluster-api \
  bastion --api-url http://localhost:3150 cluster namespaces remove --key compose-demo
```

## Register Bastion Nodes

The cluster API schedules environments onto normal Bastion host API nodes. Each
node must run `bastion start daemon` and `bastion start api`, and the node URL
must be reachable from the `cluster-api` container.

Register a node through the Compose-managed cluster API, replacing the URL with a
real node API URL:

```sh
docker compose exec -T cluster-api \
  bastion --api-url http://localhost:3150 cluster nodes create \
  --key node-a \
  --url https://node-a.internal:3148
```

If the node API runs on the same Docker host, expose it on an address reachable
from containers, such as `host.docker.internal`, your host's LAN address, or a
private DNS name.

Build the global cluster base after registering at least one node:

```sh
docker compose exec -T cluster-api \
  bastion --api-url http://localhost:3150 base build
```

The cluster stores the base in MinIO and synchronizes it to every registered
node. Base commands are global and do not use a namespace.

After building the base and creating a namespace, use the cluster API URL and
namespace with the normal Bastion resource commands:

```sh
bastion client set api-url http://localhost:3150
bastion client set namespace-key team-a
bastion templates list
bastion env list
```

## Stop and Remove Data

Stop the containers while keeping the Postgres and MinIO volumes:

```sh
docker compose down
```

Remove the containers and local data volumes:

```sh
docker compose down -v
```

## Security Notes

This example publishes service ports on the Docker host and uses demo
credentials. Change the Postgres password, MinIO credentials, bucket name, and
network exposure before using a similar Compose file outside local development.
