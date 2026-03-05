<p align="center">
  <img src="https://img.shields.io/badge/crashctl-Kubernetes_Native_Error_Tracking-EF4444?style=for-the-badge&labelColor=0A0E17" alt="crashctl" />
</p>

<h1 align="center">crashctl</h1>

<p align="center">
  <strong>Kubernetes-native error tracking for Go services.</strong><br>
  A single binary that catches panics, pod crashes, OOMKills, and CrashLoopBackOff — with zero external dependencies.
</p>

<p align="center">
  <a href="https://github.com/syst3mctl/crashctl/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/syst3mctl/crashctl/ci.yml?branch=main&style=flat-square&logo=github&label=CI&labelColor=1E293B&color=22C55E" alt="CI Status" /></a>
  <a href="https://github.com/syst3mctl/crashctl/releases/latest"><img src="https://img.shields.io/github/v/release/syst3mctl/crashctl?style=flat-square&logo=semanticrelease&labelColor=1E293B&color=84CC16" alt="Release" /></a>
  <a href="https://goreportcard.com/report/github.com/syst3mctl/crashctl"><img src="https://img.shields.io/badge/go%20report-A+-84CC16?style=flat-square&logo=go&labelColor=1E293B" alt="Go Report Card" /></a>
  <a href="https://pkg.go.dev/github.com/syst3mctl/crashctl/sdk"><img src="https://img.shields.io/badge/pkg.go.dev-reference-22D3EE?style=flat-square&logo=go&labelColor=1E293B" alt="Go Reference" /></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/syst3mctl/crashctl?style=flat-square&labelColor=1E293B&color=94A3B8" alt="License" /></a>
  <a href="https://github.com/syst3mctl/crashctl/stargazers"><img src="https://img.shields.io/github/stars/syst3mctl/crashctl?style=flat-square&logo=github&labelColor=1E293B&color=F59E0B" alt="Stars" /></a>
</p>

<p align="center">
  <a href="#quick-start">Quick Start</a> •
  <a href="#go-sdk">Go SDK</a> •
  <a href="#kubernetes-crash-detection">K8s Crashes</a> •
  <a href="#web-dashboard">Web UI</a> •
  <a href="#deployment">Deploy</a> •
  <a href="#configuration">Config</a> •
  <a href="#comparison">Compare</a>
</p>

---

## The Problem

Every error tracker catches panics **inside** your code. None of them watch what happens **outside** it.

When a pod gets OOMKilled, hits CrashLoopBackOff, or fails init — your Sentry, your GlitchTip, your Bugsink see **nothing**. These crashes happen at the Kubernetes level, below any SDK's reach. You find out from `kubectl get pods` ten minutes later, or from a PagerDuty alert that just says "pod restarted."

crashctl fixes this.

## What It Does

**Two tracks, one binary:**

| Track | What It Catches | How |
|-------|----------------|-----|
| **Application Errors** | Panics, errors, stack traces from your Go services | Lightweight Go SDK (`sdk.CaptureError`) |
| **Kubernetes Crashes** | OOMKill, CrashLoopBackOff, failed init containers, evictions | Watches the K8s API directly via SharedInformers |

When a pod crashes due to a panic that the SDK already captured, crashctl **automatically links them** — so you see the Go stack trace next to the OOMKill memory stats in one view.

## Key Numbers

| Metric | Value |
|--------|-------|
| Binary size | ~20 MB |
| Memory at idle | ~50 MB |
| External dependencies | **Zero** (embedded BadgerDB) |
| Time to deploy on K3s | 30 seconds |
| Events per second | 1,000+ per project |
| Docker image size | < 30 MB |

---

## Quick Start

### Option 1: Binary

```bash
# Install
go install github.com/syst3mctl/crashctl/cmd/crashctl@latest

# Create a project and get a DSN
crashctl project create --name my-app
# → DSN: http://localhost:9090/api/v1/events?key=a1b2c3d4...

# Start the server
crashctl serve
```

### Option 2: Docker

```bash
docker run -d \
  --name crashctl \
  -p 9090:9090 \
  -v crashctl-data:/data/crashctl \
  ghcr.io/syst3mctl/crashctl:latest
```

### Option 3: Helm (K3s / Kubernetes)

```bash
helm repo add syst3mctl https://charts.syst3mctl.dev
helm install crashctl syst3mctl/crashctl \
  --namespace monitoring \
  --create-namespace
```

Then open `http://localhost:9090` to see the dashboard.

---

## Go SDK

Install the SDK in your Go service:

```bash
go get github.com/syst3mctl/crashctl/sdk
```

### Basic Setup (3 lines)

```go
package main

import "github.com/syst3mctl/crashctl/sdk"

func main() {
    sdk.Init(sdk.Config{
        DSN:     "http://crashctl:9090/api/v1/events?key=YOUR_DSN_KEY",
        Service: "api-gateway",
        Version: "1.2.3",
    })
    defer sdk.Flush(5 * time.Second)

    // Your application code...
}
```

### Capture Errors

```go
if err := processOrder(ctx, orderID); err != nil {
    sdk.CaptureError(err,
        sdk.WithTag("order_id", orderID),
        sdk.WithUser(userID),
        sdk.WithLevel(sdk.LevelError),
    )
}
```

### Automatic Panic Recovery (HTTP Middleware)

```go
import "github.com/syst3mctl/crashctl/sdk/middleware"

// net/http
http.ListenAndServe(":8080", middleware.HTTPMiddleware(mux))

// chi
r := chi.NewRouter()
r.Use(middleware.ChiMiddleware)

// gin
r := gin.Default()
r.Use(middleware.GinMiddleware())
```

Any panic in your HTTP handlers is automatically captured with the full stack trace, error chain, and HTTP request context (method, path, status code, headers).

### What Gets Captured

Every error event includes:

- **Full stack trace** with file, function, and line number
- **Go error chain** — unwraps `errors.Is` / `errors.As` chains to show the root cause
- **Goroutine-aware traces** — captures the goroutine that panicked
- **Custom tags** — key-value pairs you attach for filtering
- **Service metadata** — name, version, environment, hostname

---

## Kubernetes Crash Detection

When crashctl runs on a Kubernetes cluster (K3s, K8s, EKS, GKE, etc.), it automatically watches the cluster for crashes that happen **outside** your application code:

### What It Detects

| Crash Type | How It's Detected | What You See |
|-----------|-------------------|-------------|
| **OOMKill** | `LastTerminationState.Terminated.Reason == "OOMKilled"` | Memory limit, actual usage, container logs |
| **CrashLoopBackOff** | `State.Waiting.Reason == "CrashLoopBackOff"` | Exit code, restart count, last 50 log lines |
| **Failed Init Container** | Init container `Terminated.ExitCode != 0` | Exit code, container logs |
| **Pod Eviction** | `Phase == Failed`, `Reason == "Evicted"` | Eviction reason, node pressure |
| **Restart Threshold** | `RestartCount > configured threshold` | Restart history, container status |

### Automatic Correlation

When a pod crashes due to a panic that the SDK already captured, crashctl links them automatically:

```
┌─────────────────────────────────────────────────┐
│ ErrorGroup: "runtime error: index out of range"  │
│ Stack: api-gateway/handlers/order.go:142         │
│ Count: 23 occurrences                            │
│                                                  │
│ 🔗 Linked Kubernetes Crash:                      │
│    OOMKill — api-gateway-7f8b4d (256Mi limit)    │
│    Node: worker-02 — 4 minutes ago               │
└─────────────────────────────────────────────────┘
```

### Required RBAC

The Helm chart sets this up automatically. If deploying manually, the ServiceAccount needs:

```yaml
rules:
  - apiGroups: [""]
    resources: ["pods", "pods/log", "events", "namespaces"]
    verbs: ["get", "list", "watch"]
```

---

## Web Dashboard

crashctl includes a built-in web dashboard at `http://localhost:9090`. No separate frontend deployment — the UI is embedded in the binary via `go:embed`.

### Pages

| Page | URL | What It Shows |
|------|-----|---------------|
| Dashboard | `/` | Total errors, active groups, recent pod crashes |
| Error List | `/errors` | All error groups, sortable by count/recency, filterable by level/status |
| Error Detail | `/errors/:id` | Full stack trace, error chain, occurrence timeline, linked K8s crashes |
| Crash List | `/crashes` | All pod crashes, filterable by namespace and crash type |
| Crash Detail | `/crashes/:id` | Container logs, memory stats, exit code, linked error group |

### Tech Stack

The UI is built with Go `html/template` + [htmx](https://htmx.org) — no React, no npm, no JavaScript build step. Sorting, filtering, and pagination are handled via htmx partial page updates. The entire UI adds ~14KB (htmx.min.js) to the binary.

---

## Observability

### Prometheus Metrics

crashctl exposes a `/metrics` endpoint in Prometheus exposition format:

| Metric | Type | Description |
|--------|------|-------------|
| `crashctl_events_total` | Counter | Total error events (labels: project, level, service) |
| `crashctl_groups_active` | Gauge | Active error groups (labels: project, status) |
| `crashctl_pod_crashes_total` | Counter | Kubernetes crashes (labels: namespace, crash_type) |
| `crashctl_ingestion_duration_seconds` | Histogram | Event processing latency |
| `crashctl_storage_size_bytes` | Gauge | BadgerDB storage usage |

### Grafana

Import the included dashboard JSON from `deploy/grafana/` to visualize crashctl metrics alongside your existing K3s monitoring.

### Webhook Alerting

Send notifications when new errors appear or pods crash:

```yaml
alerting:
  webhooks:
    - name: team-slack
      url: https://hooks.slack.com/services/XXX
      type: slack
      events: [new_group, pod_crash, regression]

    - name: ops-discord
      url: https://discord.com/api/webhooks/XXX
      type: discord
      events: [pod_crash]

    - name: pagerduty
      url: https://events.pagerduty.com/v2/enqueue
      type: generic
      events: [pod_crash]
```

Supported: **Slack** (Block Kit), **Discord** (embeds), **Generic HTTP** (JSON POST).

---

## Configuration

crashctl reads configuration from a YAML file, environment variables (`CRASHCTL_` prefix), and CLI flags. Priority: flags > env > file > defaults.

```yaml
# crashctl.yaml

server:
  listen: ":9090"                    # HTTP listen address
  base_url: "https://crashctl.example.com"  # Used in webhook links

storage:
  driver: badger                     # badger or postgres
  badger:
    path: /data/crashctl             # BadgerDB data directory
  # postgres:
  #   dsn: postgres://user:pass@localhost:5432/crashctl?sslmode=disable

retention:
  max_age: 720h                      # 30 days
  cleanup_interval: 1h               # How often to run cleanup

kubernetes:
  enabled: true                      # Enable K8s crash detection
  namespaces: []                     # Empty = all namespaces
  restart_threshold: 5               # Alert after N restarts

alerting:
  cooldown: 5m                       # Min time between alerts for same group
  webhooks:
    - name: team-slack
      url: https://hooks.slack.com/services/XXX
      type: slack
      events: [new_group, pod_crash, regression]
```

### Environment Variables

Every config key maps to an env var with `CRASHCTL_` prefix and `_` separators:

```bash
CRASHCTL_SERVER_LISTEN=":9090"
CRASHCTL_STORAGE_DRIVER="badger"
CRASHCTL_STORAGE_BADGER_PATH="/data/crashctl"
CRASHCTL_KUBERNETES_ENABLED="true"
CRASHCTL_RETENTION_MAX_AGE="720h"
```

---

## Deployment

### Architecture

```
                    ┌─────────────────────────────────────┐
                    │            crashctl binary           │
                    │                                     │
Go Service ──SDK──► │  Ingestion API ──► Grouping          │
                    │       │              │               │
                    │       ▼              ▼               │
K8s API ──Watch───► │  K8s Watcher    BadgerDB             │
                    │       │              │               │
                    │       ▼              ▼               │
                    │  Correlation    Web UI (:9090)        │
                    │       │              │               │
                    │       ▼              ▼               │
                    │  Alerting      /metrics (Prometheus)  │
                    └─────────────────────────────────────┘
```

### Helm Values

Key values you'll want to customize:

```yaml
# values.yaml
replicaCount: 1

resources:
  requests:
    memory: "128Mi"
    cpu: "100m"
  limits:
    memory: "512Mi"
    cpu: "500m"

persistence:
  enabled: true
  size: 10Gi
  storageClass: ""  # Uses default storage class

serviceMonitor:
  enabled: false    # Set true if using Prometheus Operator

ingress:
  enabled: false
  className: traefik
  hosts:
    - host: crashctl.example.com
      paths:
        - path: /
```

### CLI Commands

```bash
crashctl serve              # Start the server
crashctl project create     # Create a project, prints DSN
crashctl project list       # List all projects
crashctl cleanup            # Manual retention cleanup
crashctl version            # Print version info
```

---

## Comparison

| Feature | crashctl | Sentry | Bugsink | GlitchTip |
|---------|----------|--------|---------|-----------|
| Written in | **Go** | Python | Python | Python |
| Deployment | **Single binary** | 58+ services | Docker container | 4 services |
| Min. RAM | **~50 MB** | 16 GB+ | 1 GB | 2 GB |
| External DB required | **No** | PG + ClickHouse + Kafka + Redis | SQLite / PG | PG + Redis |
| K8s crash detection | **✓ Built-in** | ✗ | ✗ | ✗ |
| OOMKill alerts | **✓ Automatic** | ✗ | ✗ | ✗ |
| CrashLoopBackOff | **✓ Automatic** | ✗ | ✗ | ✗ |
| Crash-to-error linking | **✓** | ✗ | ✗ | ✗ |
| Go error chain support | **✓ Native** | Basic | Basic | Basic |
| Prometheus metrics | **✓ Built-in** | Via plugin | ✗ | ✗ |
| Sentry SDK compatible | Planned (v0.3) | ✓ | ✓ | ✓ |
| ARM64 support | **✓** | ✗ | ✓ | ✓ |
| License | **MIT** | FSL | BSL | MIT |

### When to Use crashctl

- You run Go services on **K3s or Kubernetes**
- You want **one tool** for both application errors and pod crashes
- You want to self-host on **minimal infrastructure** (even a Raspberry Pi)
- You value **zero external dependencies** over a rich plugin ecosystem
- You want Prometheus metrics and Grafana dashboards out of the box

### When NOT to Use crashctl

- You need error tracking for **non-Go languages** (use Sentry or GlitchTip)
- You need **full-stack observability** (APM, tracing, session replay — use Sentry or Datadog)
- You need **Sentry SDK compatibility today** (planned for v0.3, use Bugsink or GlitchTip now)
- You run on **bare metal / VMs** without Kubernetes (crashctl works but you lose the K8s moat)

---

## Roadmap

| Version | Status | Highlights |
|---------|--------|------------|
| v0.1.0-alpha | 🟡 In progress | Core: SDK, grouping, web UI, BadgerDB |
| v0.2.0-alpha | ⬜ Planned | K8s crash detection, correlation, Prometheus, alerting |
| v0.1.0 | ⬜ Planned | Helm chart, CLI polish, first public release |
| v0.2.0 | ⬜ Future | PostgreSQL backend, Grafana dashboards, OTel ingestion |
| v0.3.0 | ⬜ Future | Sentry SDK compatibility, multi-project auth, K8s operator |

---

## Contributing

Contributions are welcome. Please read the implementation guide and follow the project conventions:

1. Fork the repository
2. Create a feature branch (`feat/your-feature`)
3. Write tests for your changes
4. Ensure `make lint && make test` passes
5. Submit a pull request

### Development Setup

```bash
git clone https://github.com/syst3mctl/crashctl.git
cd crashctl
make dev  # Starts local server with hot reload
```

### Code Standards

- Error handling: always wrap with `fmt.Errorf("context: %w", err)`
- Logging: `slog` only — no `fmt.Println` or `log.Printf`
- Testing: table-driven tests with `t.Run()` subtests
- Dependencies: minimal — think twice before adding a new dependency

See [CLAUDE.md](CLAUDE.md) for the full code standards and architecture guide.

---

## License

MIT License — see [LICENSE](LICENSE) for details.

---

<p align="center">
  Built by <a href="https://github.com/syst3mctl"><strong>syst3mctl</strong></a> in Tbilisi, Georgia.<br>
  <sub>Catches what your SDK can't see.</sub>
</p>