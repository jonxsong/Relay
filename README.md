<div align="center">
  <img src="docs/assets/relay-mark.svg" width="88" alt="Relay logo" />
  <h1>Relay</h1>
  <p><strong>A durable execution engine for AI agent workflows.</strong></p>
  <p>Versioned DAGs · crash-safe workers · replayable events · observable costs</p>

  [![CI](https://github.com/jonxsong/Relay/actions/workflows/ci.yml/badge.svg)](https://github.com/jonxsong/Relay/actions/workflows/ci.yml)
  [![Go](https://img.shields.io/badge/Go-1.27.4-00ADD8?logo=go&logoColor=white)](https://go.dev/)
  [![PostgreSQL](https://img.shields.io/badge/PostgreSQL-17+-4169E1?logo=postgresql&logoColor=white)](https://www.postgresql.org/)
  [![NATS](https://img.shields.io/badge/NATS-JetStream-27AAE1?logo=natsdotio&logoColor=white)](https://nats.io/)
  [![License](https://img.shields.io/badge/license-Apache--2.0-7C3AED)](LICENSE)
</div>

> [!IMPORTANT]
> Relay is under active construction. The durable backend, live developer console, production bearer boundary, OpenAI adapter, and guarded MCP boundary are runnable. Integration, browser, load, and deployment-hardening acceptance suites are still being completed; do not treat this release as production-ready yet.

Relay turns an immutable workflow definition into a durable, inspectable run. PostgreSQL owns the truth. NATS JetStream wakes workers up. Every claim is fenced, every retry is bounded, and every user-visible state change is recorded as a replayable event.

## The product

Relay is not a marketing page or a passive agent monitor. It is the operator console and execution runtime for defining immutable DAGs, submitting work, and watching durable state move through the system.

- Create validated LLM, tool, transform, and join workflows.
- Submit idempotent runs against deterministic mock or OpenAI providers.
- Watch the live dependency graph over replayable server-sent events.
- Inspect node state, retries, event history, tokens, and known cost.
- Cancel work durably without pretending remote side effects are exactly-once.

The separate [build-status site](https://relay-build-status.perkyaisle4.chatgpt.site) reports project progress only. The Relay console itself is served by this repository and is the application intended for `relay.jonzhang.dev`.

## Why Relay

Most agent demos stop at orchestration. Relay starts where the happy path ends:

| Promise | How Relay earns it |
| --- | --- |
| Survives worker crashes | Leased attempts, fencing tokens, and recovery scans |
| Handles duplicate delivery | Transactional claims and stable message deduplication IDs |
| Explains every run | Append-only events, structured logs, attempts, traces, and usage |
| Executes DAGs concurrently | Dependency-aware scheduling with per-run and global limits |
| Controls AI risk | Typed outputs, budgets, deadlines, allowlisted tools, permission gates |
| Never overclaims | At-least-once dispatch; external side effects are not exactly-once |

## Architecture

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/relay-architecture.svg">
  <img src="docs/assets/relay-architecture.svg" alt="Relay architecture: console and API write durable workflow state to PostgreSQL, an outbox dispatcher publishes tasks to NATS, workers execute AI and MCP nodes, and the control plane recovers expired leases" width="100%">
</picture>

The diagram is the canonical system map used across Relay's documentation and build dashboard. See [Architecture](docs/architecture.md), the [OpenAPI 3.1 contract](docs/openapi.yaml), and [ADR-0001](docs/adr/0001-postgres-source-of-truth.md).

## Five-minute quickstart

Start the backend stack with:

```bash
cp .env.example .env
docker compose up --build
```

Open the console at [http://localhost:3000](http://localhost:3000), then verify the API:

```bash
curl http://localhost:8080/api/v1/health/ready
```

The console creates workflows and submits real runs; it does not seed fake metrics. In production set `RELAY_ENV=production` and provide a one-line `RELAY_API_KEY` (or mounted `RELAY_API_KEY_FILE`) with at least 32 random characters. Development mode uses the configured local developer identity.

The deterministic mock model is the default. To opt into OpenAI, set `RELAY_PROVIDER=openai` and provide `OPENAI_API_KEY` or mount a secret file through `OPENAI_API_KEY_FILE`. Optional structured outputs are checked again locally after the provider responds. Pricing is loaded only from a versioned file selected by `RELAY_PRICING_FILE`; [`config/pricing.example.json`](config/pricing.example.json) intentionally contains no invented rates, so unknown costs stay unknown. Tool nodes remain fail-closed unless `RELAY_MCP_CONFIG_FILE` points to an explicit connection and tool allowlist; use [`config/mcp.example.json`](config/mcp.example.json) as the schema. Secrets are referenced by environment-variable name and are never stored in workflow JSON.

## The execution contract

```text
define immutable version
        ↓
submit run + idempotency key
        ↓
schedule ready nodes in one database transaction
        ↓
publish outbox records to JetStream
        ↓
claim fenced attempt → execute → commit completion
        ↓
append event → unlock dependants → stream replay to UI
```

- PostgreSQL is authoritative; JetStream is a dispatch accelerator.
- A worker completion is accepted only for the current lease fencing token.
- Run and node transitions are explicit and validated.
- Cancellation is durable and prevents new work; already-started external side effects may still finish.
- Pricing is configuration, not code. Unknown model pricing produces unknown cost, never a guess.

## Repository map

```text
cmd/api              HTTP API and SSE
cmd/controlplane     scheduler, outbox dispatcher, recovery
cmd/worker           isolated node executor
internal/domain      workflow and state-machine invariants
internal/store       PostgreSQL transactions and generated queries
internal/ai          model provider boundary
internal/mcp         allowlisted tool execution
internal/telemetry   traces, metrics, structured logging
migrations           Goose SQL migrations
web                  Next.js developer console
examples             runnable workflow definitions
deploy               local and production deployment assets
docs                 architecture, ADRs, threat model, operations
```

## Development

Relay targets Go 1.27.4, Node.js 24, PostgreSQL 17+, and NATS JetStream. Local development is container-first so host toolchain drift does not change test results.

```bash
make bootstrap
make test
make dev
```

Exact commands and verified results are recorded as milestones land. See [Implementation plan](docs/implementation-plan.md) for scope and acceptance gates.

## Reliability, without mythology

Relay provides durable state transitions and duplicate-safe execution bookkeeping. It deliberately does **not** claim exactly-once execution or exactly-once external side effects. Tool authors should provide idempotency keys or compensating actions when a side effect matters.

## Security posture

- Credentials are referenced, never embedded in workflow JSON.
- LLM and tool output is untrusted and size-bounded.
- Remote MCP endpoints are allowlisted and protected against SSRF.
- Side-effecting tools require an explicit permission decision.
- Logs, traces, and API errors pass through redaction.

The full threat model and deployment hardening guide will be acceptance-gated before the production-ready milestone.

## Project status

| Area | Status |
| --- | --- |
| Architecture and ADRs | 🟢 Implemented |
| Domain invariants | 🟢 Implemented and unit tested |
| Durable scheduler and recovery | 🟡 Implemented; integration tests in progress |
| OpenAI adapter | 🟢 Implemented and contract tested |
| MCP adapter | 🟡 Allowlist, schema, approval, SSRF, and size gates implemented; live smoke test pending |
| Developer console | 🟢 Live overview, DAG, events, usage, create and run actions |
| Production authentication | 🟢 Constant-time bearer validation; user-facing access flow in progress |
| OpenAPI 3.1 | 🟢 Published and linted |
| Integration, browser, and load tests | ⚪ Planned |

## Contributing

Small, reviewable commits are preferred. Start with [CONTRIBUTING.md](CONTRIBUTING.md), preserve the durability invariants, and include a test that would fail without your change.

<div align="center">
  <sub>Built for the awkward moment between “the demo worked” and “the service stayed up.”</sub>
</div>

