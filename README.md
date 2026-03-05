# crashctl

[![CI](https://github.com/syst3mctl/crashctl/actions/workflows/ci.yml/badge.svg)](https://github.com/syst3mctl/crashctl/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/badge/go-1.23%2B-00ADD8?logo=go)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/syst3mctl/crashctl)](https://goreportcard.com/report/github.com/syst3mctl/crashctl)

**Self-hosted error tracking with built-in Kubernetes crash detection.**

crashctl is a single static binary that captures application errors, groups them by root cause, and automatically detects Kubernetes pod crashes — OOMKills, CrashLoopBackOffs, evictions — without any agents or sidecars. Everything ships in one binary: HTTP ingest API, web UI, K8s watcher, and Prometheus metrics endpoint.

---

## Why crashctl

Most error trackers are SaaS tools that require sending production data to a third party, or self-hosted tools that need Redis, Postgres, and a message queue just to get started. crashctl needs nothing but a volume and a K8s ServiceAccount.

The differentiator is the **Kubernetes crash watcher**: crashctl watches the K8s API directly and correlates pod crashes (OOMKill, CrashLoopBackOff) with SDK-reported errors. When a pod crashes, you see both the K8s event and the Go panic that caused it — linked automatically.

| | crashctl | Sentry (self-hosted) | GlitchTip |
|---|---|---|---|
| Single binary | ✅ | ❌ | ❌ |
| No external dependencies | ✅ | ❌ | ❌ |
| K8s OOMKill detection | ✅ | ❌ | ❌ |
| CrashLoopBackOff detection | ✅ | ❌ | ❌ |
| Crash → error linking | ✅ | ❌ | ❌ |
| Embedded web UI | ✅ | ✅ | ✅ |
| Prometheus metrics | ✅ | ✅ | ❌ |

---

## Features

- **Error grouping** — SHA-256 fingerprint of normalized Go stack frames. Identical panics produce one group with an accurate occurrence count, regardless of goroutine IDs or memory addresses.
- **Kubernetes crash detection** — SharedInformer watches pods across all (or configured) namespaces. Detects OOMKill, CrashLoopBackOff, failed init containers, evictions, and restart threshold breaches.
- **Crash-to-error correlation** — Links a `PodCrash` to its `ErrorGroup` using pod hostname + time window matching. The error detail page shows the K8s crash; the crash detail page links back to the error group.
- **Go SDK** — `sdk.CaptureError`, `sdk.Recover`, and HTTP middleware for chi, gin, and `net/http`. Async send with exponential backoff. Never blocks your application.
- **Web UI** — Go templates + htmx. No React, no npm, no build step. The binary is fully self-contained.
- **Prometheus metrics** — `/metrics` endpoint with counters for events, groups, pod crashes, and ingestion latency.
- **Webhook alerting** — Slack, Discord, and generic HTTP webhooks for new error groups, pod crashes, and regressions.

---

## Quick Start

### Docker

```bash
docker run -d \
  --name crashctl \
  -p 9090:9090 \
  -v crashctl-data:/data/crashctl \
  syst3mctl/crashctl:latest
```

Open `http://localhost:9090`. Create a project:

```bash
docker exec crashctl /crashctl project create --name "my-service"
# DSN: http://localhost:9090 | Key: <your-dsn-key>
```

### Kubernetes (Helm)

```bash
helm install crashctl oci://ghcr.io/syst3mctl/charts/crashctl \
  --namespace monitoring \
  --create-namespace \
  --set config.kubernetes.namespaces="{default,production}"
```

The Helm chart configures the ServiceAccount with the required RBAC (get/list/watch on pods and pod logs) automatically.

### Binary

```bash
curl -L https://github.com/syst3mctl/crashctl/releases/latest/download/crashctl-linux-amd64 \
  -o /usr/local/bin/crashctl && chmod +x /usr/local/bin/crashctl

crashctl serve --config crashctl.yaml
```

---

## SDK

```bash
go get github.com/syst3mctl/crashctl/sdk
```

```go
package main

import (
    "github.com/syst3mctl/crashctl/sdk"
)

func main() {
    sdk.Init(sdk.Config{
        DSN:     "http://localhost:9090",
        DSNKey:  "your-project-key",
        Service: "my-service",
        Version: "1.0.0",
    })
    defer sdk.Flush(5 * time.Second)

    // Capture errors
    if err := doWork(); err != nil {
        sdk.CaptureError(err, sdk.WithTag("job", "nightly-sync"))
    }

    // Capture panics in goroutines
    go func() {
        defer sdk.Recover()
        riskyOperation()
    }()
}
```

### HTTP Middleware

```go
// net/http
mux.Handle("/", crashmiddleware.HTTPMiddleware(yourHandler))

// chi
r.Use(crashmiddleware.ChiMiddleware)

// gin
r.Use(crashmiddleware.GinMiddleware())
```

Middleware automatically captures panics with the full HTTP request context (method, path, status code, client IP) and re-panics so your existing recovery handler still fires.

---

## Configuration

```yaml
# crashctl.yaml
server:
  listen: ":9090"
  base_url: "https://crashctl.example.com"

storage:
  driver: badger
  badger:
    path: /data/crashctl

retention:
  max_age: 720h          # 30 days
  cleanup_interval: 1h

kubernetes:
  enabled: true
  namespaces: []         # empty = all namespaces
  restart_threshold: 5

alerting:
  webhooks:
    - name: team-slack
      url: https://hooks.slack.com/services/XXX
      type: slack
      events: [new_group, pod_crash, regression]
```

All keys can be overridden with `CRASHCTL_` environment variables (e.g. `CRASHCTL_SERVER_LISTEN=:8080`) or CLI flags. Priority: flags > env vars > config file > defaults.

---

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        crashctl binary                       │
│                                                             │
│  ┌──────────────┐  ┌──────────────┐  ┌───────────────────┐ │
│  │  HTTP API    │  │   Web UI     │  │  K8s Watcher      │ │
│  │              │  │  (htmx +     │  │  (SharedInformer) │ │
│  │ POST /events │  │  templates)  │  │                   │ │
│  │ GET  /health │  │              │  │  OOMKill          │ │
│  │ GET  /metrics│  │ /errors      │  │  CrashLoopBackOff │ │
│  └──────┬───────┘  │ /errors/:id  │  │  Eviction         │ │
│         │          │ /crashes     │  │  Init fail        │ │
│  ┌──────▼───────┐  │ /crashes/:id │  └────────┬──────────┘ │
│  │  Grouping    │  └──────────────┘           │            │
│  │ (SHA-256     │                             │            │
│  │  fingerprint)│  ┌──────────────────────────▼──────────┐ │
│  └──────┬───────┘  │           BadgerDB                  │ │
│         └──────────►  events / groups / crashes / projects│ │
│                    └─────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

The storage layer is behind a `Store` interface — a BadgerDB implementation ships in the binary; a PostgreSQL implementation is on the roadmap.

### Key schema (BadgerDB)

| Prefix | Key | Value |
|---|---|---|
| `e:` | `e:{projectID}:{timestamp_ns}:{eventID}` | JSON `Event` |
| `g:` | `g:{projectID}:{groupID}` | JSON `ErrorGroup` |
| `f:` | `f:{projectID}:{fingerprint}` | `groupID` bytes |
| `c:` | `c:{namespace}:{timestamp_ns}:{crashID}` | JSON `PodCrash` |
| `p:` | `p:{projectID}` | JSON `Project` |

ULID keys are lexicographically sortable, so prefix range scans return results in chronological order with no secondary index.

---

## CLI

```
crashctl serve              Start the server (web UI + API + K8s watcher)
crashctl project create     Create a project and print its DSN key
crashctl project list       List all projects
crashctl cleanup            Manually trigger retention cleanup
crashctl version            Print version, commit, and build date
```

---

## Metrics

| Metric | Type | Labels |
|---|---|---|
| `crashctl_events_total` | Counter | `project`, `level`, `service` |
| `crashctl_groups_active` | Gauge | `project`, `status` |
| `crashctl_pod_crashes_total` | Counter | `namespace`, `crash_type` |
| `crashctl_ingestion_duration_seconds` | Histogram | — |
| `crashctl_storage_size_bytes` | Gauge | — |

---

## Development

**Requirements:** Go 1.23+

```bash
git clone https://github.com/syst3mctl/crashctl
cd crashctl

make build   # Build binary
make test    # Run tests with race detector
make lint    # golangci-lint
make dev     # go run ./cmd/crashctl serve
```

---

## Roadmap

- **PostgreSQL backend** — for teams with existing PG infrastructure
- **Sentry SDK compatibility** — accept Sentry wire format for zero-code migration
- **OpenTelemetry ingestion** — accept OTel error spans and enrich with K8s context
- **Grafana dashboard templates** — pre-built dashboards for crashctl metrics
- **Multi-user auth** — OIDC/SSO for team access control

---

## License

[MIT](LICENSE) © 2026 syst3mctl
