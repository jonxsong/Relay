# ADR-0001: PostgreSQL is the workflow source of truth

- Status: accepted
- Date: 2026-11-14

## Context

Relay must survive process failure, duplicate task delivery, scheduler races, and broker outages without inventing multiple conflicting workflow histories.

## Decision

PostgreSQL is authoritative for workflow and execution state. JetStream carries notifications whose payloads identify persisted attempts. A transactional outbox couples state transitions to publication intent. Consumers validate every message against current database state.

## Consequences

- Broker loss delays work but does not erase the decision to perform it.
- Duplicate and stale messages become harmless database predicate failures.
- Scheduling requires careful short transactions and indexes.
- Relay does not depend on broker ordering for correctness.

