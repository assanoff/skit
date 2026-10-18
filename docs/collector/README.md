# OpenTelemetry Collector for skit services

[`collector.yaml`](collector.yaml) is a reference Collector config for skit
services in Kubernetes. It takes the spans the services export and the logs
they write, adds the pod, namespace and workload to both, and sends them to
one OTLP backend (Tempo, Jaeger, an OTLP gateway). Copy it and set the backend.

## What a skit service emits

| signal | how | where it goes |
|---|---|---|
| spans | `otel/otlp` exports OTLP/gRPC when `OTEL_TRACES_EXPORTER=otlp`, to `OTEL_EXPORTER_OTLP_ENDPOINT` | the `otlp` receiver of the collector |
| logs | `logger.New`: one JSON record per line on stdout — `time`, `level`, `msg`; `trace_id`, `span_id`, `trace_flags` at the top level of a record written in a span; `service.name`, `service.version` | the container log under `/var/log/pods`, read by `file_log` |
| metrics | Prometheus text on `/metrics` of the debug server (`debugsrv`) | scraped by Prometheus directly, not through the collector |

## Pipelines

- **traces**: `otlp` → `memory_limiter` → `k8s_attributes` → `batch` →
  `otlp_grpc`.
- **logs**: `file_log` → `memory_limiter` → `k8s_attributes` → `batch` →
  `otlp_grpc`. The `file_log` operators turn a line into an OpenTelemetry log
  record:
  - `container` — the runtime's line format; `k8s.namespace.name`,
    `k8s.pod.name`, `k8s.pod.uid`, `k8s.container.name` from the file path;
  - `json_parser` — the slog record into attributes, `time` into the
    timestamp, `level` into the severity; a line that is not JSON stays the
    body;
  - `trace_parser` — `trace_id`, `span_id`, `trace_flags` into the record's
    trace context, so a log record and its span are linked in the backend;
  - `move` — `service.name` and `service.version` into the Resource, named as
    on the spans; `msg` into the body.

## Running it

- A **DaemonSet**, one collector per node: `file_log` reads only its node's
  logs. Mount `/var/log/pods` read-only from the host. The container is named
  `otel-collector`; its own logs are excluded by that name.
- **Spans**: expose port 4317 on the node (a `hostPort`, or a Service with
  `internalTrafficPolicy: Local`) and give the services
  `OTEL_EXPORTER_OTLP_ENDPOINT=http://$(NODE_IP):4317`, with `NODE_IP` from
  the downward API (`status.hostIP`).
- **Backend**: `OTLP_BACKEND_ENDPOINT` (host:port of the backend's OTLP/gRPC).
- **RBAC** for `k8s_attributes`: get, list and watch `pods`, `namespaces` and
  `replicasets` (the deployment name comes from the ReplicaSet).
- `start_at: end` — a restarted collector skips the lines written while it was
  down. To keep its place, add the `file_storage` extension on a writable
  host path and set `storage` on `file_log`.

## Checking it

```sh
make collector-check   # otelcol validate in otel/opentelemetry-collector-contrib (needs docker)
```
