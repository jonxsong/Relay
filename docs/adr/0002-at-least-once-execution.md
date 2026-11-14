# ADR-0002: State at-least-once semantics explicitly

- Status: accepted
- Date: 2026-11-14

## Context

Network partitions make it impossible to know whether an external side effect completed when the response is lost. Claiming exactly-once execution would create a dangerous false guarantee.

## Decision

Relay promises at-least-once dispatch with duplicate-safe internal state transitions. Lease fencing rejects stale database completions. External tools receive stable idempotency context when possible and require explicit permission when side effects are declared.

## Consequences

- Documentation and UI expose attempts and retries.
- Tool integrations must classify side effects and retry safety.
- Operators can reason about ambiguity instead of having it hidden.

