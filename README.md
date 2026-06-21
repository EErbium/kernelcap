# AI Compute Profiler

A lightweight agent that watches over GPU servers running AI models. It spots problems like memory leaks, stuck processes, or repetitive API calls, and can automatically step in to fix them before they waste compute.

## What it does

- **Monitors** GPU and CPU usage across all running processes
- **Detects** three types of issues:
  - *Semantic repetition loops* — an AI model generating the same response repeatedly
  - *Idle GPU hogs* — processes holding GPU memory without doing useful work
  - *Host memory leaks* — processes consuming more RAM over time
- **Intervenes** automatically — throttles runaway requests, pauses containers, or freezes processes
- **Rolls back** interventions when the issue clears up
- **Proxies** AI API calls and tracks token usage per process
- **Displays** live metrics and intervention history in a browser dashboard

## Architecture

```
┌─────────────┐     ┌──────────────────┐     ┌──────────────┐
│  Collector   │────▶│  Pipeline &      │────▶│  Upstream    │
│  (GPU, CPU,  │     │  Ring Buffer     │     │  Telemetry   │
│   eBPF)      │     └──────┬───────────┘     └──────────────┘
└─────────────┘            │
                           ▼
┌─────────────┐     ┌──────────────────┐     ┌──────────────┐
│  Detector   │────▶│  Alert           │────▶│  Mitigator   │
│  Profiler   │────▶│  Multiplexer     │────▶│  Router      │
└─────────────┘     └──────┬───────────┘     └──────┬───────┘
                           │                        │
                           ▼                        ▼
                    ┌──────────────┐     ┌──────────────────┐
                    │  Webhooks    │     │  Policy Engine   │
                    │  SSE stream  │     │  Rollback        │
                    └──────────────┘     └──────────────────┘
```

- **Backend** (Go): `cmd/profiler-agent` — single binary, zero dependencies
- **Frontend** (React + TypeScript): `frontend/` — Vite-based dashboard

## Quick start

```bash
# Build the agent
cd kernelcap
go build -o profiler-agent ./cmd/profiler-agent

# Run with default settings
./profiler-agent run

# Open dashboard
cd frontend
npm install
npm run dev
```

### Configuration

Set via environment variables or a JSON/YAML config file:

| Variable | Default | Description |
|---|---|---|
| `PROFILER_POLL_INTERVAL_MS` | 500 | How often to sample metrics |
| `PROFILER_HTTP_ADDR` | :9090 | Metrics endpoint |
| `PROFILER_PROXY_ENABLED` | false | Enable AI API proxy |
| `PROFILER_DASHBOARD_ADDR` | 127.0.0.1:8088 | Dashboard API address |
| `AGENT_ID` | hostname | Node identifier |
| `UPSTREAM_ENDPOINT` | — | Telemetry destination |
| `AUTH_TOKEN` | — | Auth for upstream |

## Project structure

```
cmd/profiler-agent/    — Entry point
pkg/
  collector/           — GPU/CPU metrics collection
  profiler/            — Memory leak & idle GPU detection
  detector/            — Semantic repetition detection
  proxy/               — AI API proxy & token tracking
  router/              — Request throttling & fallback routing
  mitigator/           — Container pause, SIGSTOP, cgroup freeze
  policy/              — Authorization & rate limiting
  rollback/            — Automatic recovery
  alerter/             — Alert dedup & fanout
  webhook/             — HTTP webhook dispatch & SSE streaming
  engine/              — Orchestrator tying everything together
frontend/              — React dashboard
```
