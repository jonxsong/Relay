# Architecture

Relay is a distributed state machine for immutable agent-workflow DAGs. It separates the durable decision to perform work from the best-effort act of notifying a worker.

## Components

- **API** authenticates requests, validates bounded workflow definitions, persists idempotent submissions, and streams database-backed events.
- **Control plane** advances DAG state, materializes outbox records, publishes tasks, and recovers expired attempts.
- **Workers** claim an attempt transactionally, renew its lease, execute one typed node, and submit a fenced completion.
- **PostgreSQL** owns workflow versions, run state, attempts, events, usage, idempotency records, and the transactional outbox.
- **JetStream** transports task notifications. Losing or duplicating a notification cannot corrupt authoritative state.

## Non-negotiable invariants

1. A workflow version is immutable after creation.
2. A node becomes ready only when every dependency succeeded.
3. A node has at most one current attempt, identified by a monotonically increasing number and fencing token.
4. Only the current fencing token may renew a lease or commit completion.
5. Scheduling state and its outbox record are committed atomically.
6. User-visible state transitions append an event in the same transaction.
7. Per-run event sequence numbers increase monotonically.
8. Cancellation prevents new scheduling and makes late completions harmless.
9. Exhausted required nodes fail the run; descendants that cannot execute are skipped.
10. Run deadlines, node deadlines, and known-cost budgets terminate work durably and fence late workers.
11. Secrets never appear in workflow JSON, application logs, traces, events, or API errors.

## Delivery semantics

Relay provides at-least-once task delivery and duplicate-safe state transitions. A worker may execute after losing its lease, and an external service may observe a request even when Relay does not observe its response. Fencing protects Relay's database; it cannot make arbitrary external side effects exactly once.

Side-effecting tools therefore require explicit permission and should accept a stable idempotency key derived from the run, node, and attempt when the remote system supports it.

## Scheduling transaction

For each candidate node, the scheduler locks its run and relevant node rows, checks cancellation and dependency state, enforces concurrency and budgets, transitions `pending → ready → queued`, allocates the next attempt identity, appends an execution event, and inserts an outbox record. A conflicting scheduler observes the committed state and does no duplicate logical work.

## Claim and completion

A worker receiving a task message attempts to claim the referenced queued attempt. Claiming sets the worker identity, lease deadline, immutable node timeout, optional run deadline, and a new fencing token in one transaction. The worker uses the earliest applicable deadline. Completion compares the token and current status before writing output, usage, events, and newly schedulable work. A stale completion receives a conflict and is discarded.

## Recovery

The recovery loop scans an indexed lease deadline. Expired retryable work is moved through bounded exponential backoff with jitter. Non-retryable or exhausted work fails terminally and propagates failure to the run. The same control-plane pass terminally fails overdue runs. Cancellation, deadlines, and known-cost budget exhaustion fence active attempts so worker completions cannot resurrect stopped work. The loop is safe to run concurrently because it uses row locks and state predicates.

## Cost budgets

Configured provider costs are recorded with a pricing-version reference and summed in the same serializable transaction that commits a node result. If known cost exceeds the run budget, Relay preserves the completed node and fails remaining work with `cost_budget_exceeded`. Unknown pricing stays unknown and is never estimated, so operators should pair budgets with a versioned pricing catalog when strict monetary enforcement is required.

