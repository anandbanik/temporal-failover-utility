# temporal-utility

A lightweight HTTP service that exposes a REST API for managing [Temporal](https://temporal.io) namespaces. It wraps the Temporal gRPC `WorkflowService` so that namespace provisioning can be done over plain HTTP without requiring a Temporal SDK or direct gRPC access.

## Prerequisites

- Go 1.21+
- A running Temporal server (default: `localhost:7233`)
- [`golangci-lint`](https://golangci-lint.run/usage/install/) (for linting only)
- Docker (for container builds only)

## Build & Run

### Local

```bash
# Build binary
make build

# Run (connects to Temporal at localhost:7233, listens on :8080)
make run

# Or run the compiled binary directly
./temporal-utility
```

### Docker

```bash
# Build image (default tag: temporal-utility:latest)
make docker

# Custom tag
make docker IMAGE_TAG=myrepo/temporal-utility:v1.0.0

# Run container
docker run --rm \
  -p 8080:8080 \
  -e TEMPORAL_HOST_PORT=host.docker.internal:7233 \
  temporal-utility:latest
```

Pass any configuration as `-e` flags — see the [Configuration](#configuration) section below.

> **Note:** The final image is based on `gcr.io/distroless/static-debian12` and contains only the binary, so `docker exec` shell access is not available.

## Configuration

All configuration is via environment variables.

| Variable | Default | Description |
|---|---|---|
| `SERVER_PORT` | `8080` | HTTP listen port |
| `TEMPORAL_HOST_PORT` | `localhost:7233` | Temporal server address |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | _(unset)_ | OTLP gRPC endpoint for traces and metrics. Falls back to stdout when unset. |
| `OTEL_SERVICE_NAME` | `temporal-utility` | Service name reported in OTel telemetry |
| `OTEL_SERVICE_VERSION` | `0.1.0` | Service version reported in OTel telemetry |

Example with custom values:

```bash
SERVER_PORT=9090 \
TEMPORAL_HOST_PORT=temporal.internal:7233 \
OTEL_EXPORTER_OTLP_ENDPOINT=otel-collector:4317 \
./temporal-utility
```

## API

### Health check

```
GET /healthz
```

```json
{"status": "ok"}
```

### Promote a namespace to global

```
POST /api/v1/namespaces/:name/promote
```

Promotes a local namespace to a global (multi-cluster) namespace and optionally sets its cluster replication configuration. Equivalent to:
```
temporal operator namespace update --namespace <name> --cluster c1 --cluster c2
```

**Request body (optional):**

| Field | Type | Required | Description |
|---|---|---|---|
| `clusters` | string[] | no | Cluster names to assign to the namespace replication config |

**Example — promote only:**

```bash
curl -X POST http://localhost:9090/api/v1/namespaces/my-namespace/promote
```

**Example — promote and set clusters:**

```bash
curl -X POST http://localhost:9090/api/v1/namespaces/my-namespace/promote \
  -H 'Content-Type: application/json' \
  -d '{"clusters": ["c1", "c2"]}'
```

**Response `200 OK`:**

```json
{
  "id": "<namespace-id>",
  "name": "my-namespace",
  "is_global": true,
  "clusters": ["c1", "c2"]
}
```

> `clusters` is omitted from the response when no replication config is returned by Temporal.

| Status | Meaning |
|---|---|
| `200 OK` | Namespace updated successfully |
| `400 Bad Request` | Malformed request body |
| `404 Not Found` | Namespace does not exist |
| `500 Internal Server Error` | Temporal communication failure |

### Namespace handover

```
POST /api/v1/namespaces/:name/handover
Content-Type: application/json
```

Performs a two-step namespace handover in a single call:

1. **Updates the active cluster** of the namespace (`--active-cluster`).
2. **Starts the `namespace-handover` workflow** in `temporal-system` on the `default-worker-tq` task queue with static parameters (`AllowedLaggingSeconds: 120`, `HandoverTimeoutSeconds: 5`).

Equivalent CLI commands:
```
temporal operator namespace update --namespace <name> --active-cluster <cluster>

temporal workflow start \
  --namespace temporal-system \
  --task-queue default-worker-tq \
  --type namespace-handover \
  --input '{"Namespace":"<name>","RemoteCluster":"<cluster>","AllowedLaggingSeconds":120,"HandoverTimeoutSeconds":5}'
```

**Request body:**

| Field | Type | Required | Description |
|---|---|---|---|
| `cluster` | string | yes | Target cluster name — used as both the new active cluster and the `RemoteCluster` in the workflow input |

**Example:**

```bash
curl -X POST http://localhost:9090/api/v1/namespaces/replicationtest/handover \
  -H 'Content-Type: application/json' \
  -d '{"cluster": "btWest"}'
```

**Response `202 Accepted`:**

```json
{
  "workflow_id": "namespace-handover-replicationtest-<uuid>",
  "run_id": "<run-id>"
}
```

> The workflow runs asynchronously. Use the returned `workflow_id` and `run_id` to query its status via the Temporal UI or CLI.

| Status | Meaning |
|---|---|
| `202 Accepted` | Active cluster updated and handover workflow started |
| `400 Bad Request` | Missing or invalid request body |
| `404 Not Found` | Namespace does not exist |
| `500 Internal Server Error` | Temporal communication failure (if the active cluster update succeeded but the workflow start failed, this is logged with the workflow ID) |

### Upsert a remote cluster

```
POST /api/v1/clusters
Content-Type: application/json
```

Adds or updates a remote cluster connection. Equivalent to:
```
temporal operator cluster upsert --enable-connection --frontend-address "<address>"
```

**Request body:**

| Field | Type | Required | Description |
|---|---|---|---|
| `frontend_address` | string | yes | gRPC address of the remote cluster frontend (e.g. `temporal-east:7233`) |
| `enable_connection` | bool | no | Enable cross-cluster connection (default: `false`) |
| `frontend_http_address` | string | no | HTTP address for the remote cluster frontend. If omitted on update, the existing HTTP address is removed. |
| `enable_replication` | bool | no | Enable replication streams (default: `false`) |

**Example:**

```bash
curl -X POST http://localhost:9090/api/v1/clusters \
  -H 'Content-Type: application/json' \
  -d '{
    "frontend_address": "temporal-east:7233",
    "enable_connection": true
  }'
```

**Responses:**

| Status | Meaning |
|---|---|
| `204 No Content` | Cluster added or updated successfully |
| `400 Bad Request` | Missing or invalid request body |
| `500 Internal Server Error` | Temporal communication failure |

### Create a namespace

```
POST /api/v1/namespaces
Content-Type: application/json
```

**Request body:**

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Namespace name |
| `description` | string | no | Human-readable description |
| `owner_email` | string | no | Owner contact email |
| `retention_days` | int | no | Workflow history retention in days (default: `7`) |
| `is_global` | bool | no | Whether this is a global (multi-cluster) namespace |
| `active_cluster` | string | no | Active cluster name (for global namespaces) |
| `data` | object | no | Arbitrary key-value metadata |

**Example:**

```bash
curl -X POST http://localhost:9090/api/v1/namespaces \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "my-namespace",
    "owner_email": "team@example.com",
    "retention_days": 30
  }'
```

**Responses:**

| Status | Meaning |
|---|---|
| `201 Created` | Namespace created; body contains `{"id": "<namespace-id>"}` |
| `400 Bad Request` | Missing or invalid request body |
| `409 Conflict` | Namespace already exists |
| `500 Internal Server Error` | Temporal communication failure |

## API Documentation (Swagger UI)

The service embeds a Swagger UI at `/swagger/index.html` and serves the OpenAPI spec at `/swagger/doc.json`.

Once the server is running:

```
http://localhost:9090/swagger/index.html
```

The spec is generated from source annotations using [swaggo/swag](https://github.com/swaggo/swag). Re-generate it whenever handler signatures or request/response types change:

```bash
# Install the CLI (one-time)
go install github.com/swaggo/swag/cmd/swag@v1.16.6

# Re-generate docs/
make swag
```

The generated `docs/` directory is committed so the service can be built and run without requiring the `swag` CLI.

## Development

```bash
make swag    # regenerate OpenAPI spec (requires swag CLI)
make test    # run all tests with race detector
make lint    # run golangci-lint
make tidy    # tidy go.mod / go.sum
make clean   # remove compiled binary
```

## Observability

When `OTEL_EXPORTER_OTLP_ENDPOINT` is set, the service exports traces and metrics over OTLP gRPC (insecure). Otherwise, telemetry is printed to stdout — useful for local development.

Every HTTP request produces:
- A server-side OTel **span** with method, path, and status code attributes
- Increments to the `http.server.request.count` counter
- A latency sample in the `http.server.request.duration` histogram

Structured request logs (via Zap) include `trace_id` and `span_id` fields when a valid OTel span is active, enabling log-to-trace correlation.
