# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this service does

`temporal-utility` is a Go HTTP service that exposes a REST API for managing Temporal namespaces. It wraps Temporal's gRPC `WorkflowService` to provide a simpler HTTP interface, with built-in OpenTelemetry observability.

## Commands

```bash
# Build
go build ./...

# Run (requires a Temporal server reachable at localhost:7233 by default)
go run ./main.go

# Test
go test ./...

# Run a single test package
go test ./handlers/ -v -run TestName

# Lint (golangci-lint required)
golangci-lint run

# Regenerate OpenAPI spec — run after changing handler signatures or request/response types
# Requires: go install github.com/swaggo/swag/cmd/swag@v1.16.6
make swag
```

## Configuration (environment variables)

| Variable | Default | Description |
|---|---|---|
| `SERVER_PORT` | `8080` | HTTP listen port |
| `TEMPORAL_HOST_PORT` | `localhost:7233` | Temporal server address |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `""` | OTLP gRPC endpoint; falls back to stdout if unset |
| `OTEL_SERVICE_NAME` | `temporal-utility` | OTel resource attribute |
| `OTEL_SERVICE_VERSION` | `0.1.0` | OTel resource attribute |

## Architecture

```
main.go               — wires config → OTEL → Temporal client → HTTP server; graceful shutdown on SIGINT/SIGTERM (10s timeout)
config/config.go      — pure env-var config, no files
temporal/client.go    — creates go.temporal.io/sdk client.Client
telemetry/telemetry.go — OTel SDK setup (TracerProvider + MeterProvider); OTLP gRPC when endpoint set, stdout otherwise
router/router.go      — Gin engine with OTEL + Logger middleware; GET /healthz, POST /api/v1/namespaces
middleware/otel.go    — per-request OTel span + http.server.request.count counter + http.server.request.duration histogram
middleware/logger.go  — structured Zap request log; injects trace_id/span_id from active OTel span
handlers/namespace.go — CreateNamespace handler; calls temporal WorkflowService.RegisterNamespace gRPC directly
```

**Key design decisions:**
- Handlers call `client.Client.WorkflowService()` (raw gRPC) rather than higher-level SDK helpers, because namespace registration is a control-plane operation not covered by the workflow SDK abstractions.
- Default namespace retention is 7 days when `retention_days` is omitted or ≤ 0.
- `409 Conflict` is returned (not `500`) when a namespace already exists, detected via `*serviceerror.NamespaceAlreadyExists`.

## API

```
GET  /healthz                    → {"status":"ok"}
POST /api/v1/namespaces          → create a Temporal namespace
```

`POST /api/v1/namespaces` body:
```json
{
  "name": "my-namespace",        // required
  "description": "",
  "owner_email": "",
  "retention_days": 7,           // defaults to 7 if omitted or ≤ 0
  "is_global": false,
  "active_cluster": "",
  "data": {}
}
```

## Adding new handlers

1. Add request/response types and a handler method to a file in `handlers/`.
2. Inject any new dependencies via the constructor (follow `NewNamespaceHandler` pattern).
3. Register the route in `router/router.go`.
4. Create OTel spans inside handlers using `otel.Tracer("temporal-utility/handlers")`.
