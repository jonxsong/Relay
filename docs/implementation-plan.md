# Implementation plan

The build is organized as reviewable vertical milestones. Each milestone must keep the repository buildable and document the exact checks that passed.

## Milestone 0 — architecture foundation

- Repository conventions, architecture diagram, ADRs, and contribution policy.
- Toolchain versions verified against primary sources.
- Acceptance: links render, secret files are ignored, repository pushes cleanly.

## Milestone 1 — executable domain core

- Go module and service entry points.
- Workflow DAG validation, status transitions, retry policy, and input bindings.
- Unit tests for cycles, limits, dependencies, transitions, backoff, and cancellation.
- Acceptance: `go test -race ./...` passes in the pinned Go container.

## Milestone 2 — durable vertical slice

- PostgreSQL migrations and sqlc configuration.
- Workflow creation/versioning, idempotent run submission, scheduler, attempts, events, and SSE.
- Transactional outbox, JetStream dispatcher, worker claim/lease/completion, deterministic mock model.
- Compose environment and a runnable example.
- Acceptance: integration suite proves duplicate delivery, stale completion rejection, lease recovery, and replay.

## Milestone 3 — provider and tool boundaries

- OpenAI Responses API adapter behind a provider interface.
- Structured-output validation, timeouts, cancellation, usage, and configured pricing.
- Official MCP SDK client with endpoint allowlist, SSRF protections, scoped secrets, schemas, and side-effect permission gates.
- Acceptance: adapter contract tests and redaction tests pass without paid services; one opt-in live smoke test is documented.

## Milestone 4 — developer console

- Next.js App Router console with workflows, React Flow editor, runs, live DAG, attempts, logs, retries, usage, settings, and robust empty/error/loading states.
- Generated OpenAPI client and Zod validation at external-data boundaries.
- Acceptance: TypeScript, ESLint, production build, accessibility checks, and Playwright happy-path/cancellation flows pass.

## Milestone 5 — operational hardening

- OpenTelemetry traces, Prometheus metrics, readiness checks, rate limits, payload limits, and redaction.
- Authentication, ownership authorization, deployment guidance, threat model, dependency scanning, and backup/recovery runbooks.
- Deterministic load harness for 1, 5, 10, and 25 concurrent agents.
- Acceptance: CI gates the full suite; benchmark results are recorded from actual runs only.

## Acceptance record — 2026-11-14

| Gate | Verified result |
| --- | --- |
| Domain and API unit suite | `go vet ./...` and `go test ./...` passed |
| Data-race suite | `go test -race ./...` passed in `golang:1.27.4-bookworm` |
| Failure recovery | Real PostgreSQL tests passed: outbox crash window, redelivery, duplicate claims, lease expiry, stale completion rejection, active cancellation fencing, run/node deadlines, and recovered completion |
| Cost controls | Real PostgreSQL test passed: known provider cost over budget terminates the run and fences remaining work |
| Durable vertical slice | Real PostgreSQL + NATS run passed in 15.52s, including idempotent replay/conflict and ordered event replay |
| API contract | Generated TypeScript client matches `docs/openapi.yaml` |
| Developer console | TypeScript, ESLint, production build, and Playwright create/run/inspect/search/settings/cancel flow passed |
| Public image | `deploy/render.Dockerfile` built and its production-auth browser flow passed against the all-in-one evaluation container |
| Load harness | 80 measured deterministic runs completed; see [`benchmarks/results-local.md`](../benchmarks/results-local.md) |

The paid OpenAI call is deliberately opt-in and is documented in [`provider-smoke-test.md`](provider-smoke-test.md); automated acceptance never spends provider credits.

