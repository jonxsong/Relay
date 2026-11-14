-- name: EnsureUser :one
INSERT INTO users (email, display_name)
VALUES (sqlc.arg(email), sqlc.arg(display_name))
ON CONFLICT (email) DO UPDATE
SET display_name = EXCLUDED.display_name,
    updated_at = now()
RETURNING *;

-- name: CreateWorkflow :one
INSERT INTO workflows (owner_id, name, description)
VALUES (sqlc.arg(owner_id), sqlc.arg(name), sqlc.arg(description))
RETURNING *;

-- name: LockWorkflowForVersion :one
SELECT *
FROM workflows
WHERE id = sqlc.arg(id)
  AND owner_id = sqlc.arg(owner_id)
  AND deleted_at IS NULL
FOR UPDATE;

-- name: GetWorkflow :one
SELECT *
FROM workflows
WHERE id = sqlc.arg(id)
  AND owner_id = sqlc.arg(owner_id)
  AND deleted_at IS NULL;

-- name: ListWorkflows :many
SELECT *
FROM workflows
WHERE owner_id = sqlc.arg(owner_id)
  AND deleted_at IS NULL
  AND (sqlc.narg(before_created_at)::timestamptz IS NULL OR (created_at, id) < (sqlc.narg(before_created_at), sqlc.narg(before_id)::uuid))
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_size);

-- name: NextWorkflowVersion :one
SELECT (COALESCE(MAX(version), 0) + 1)::integer AS version
FROM workflow_versions
WHERE workflow_id = sqlc.arg(workflow_id);

-- name: CreateWorkflowVersion :one
INSERT INTO workflow_versions (workflow_id, version, definition, definition_sha256, created_by)
VALUES (sqlc.arg(workflow_id), sqlc.arg(version), sqlc.arg(definition), sqlc.arg(definition_sha256), sqlc.arg(created_by))
RETURNING *;

-- name: GetWorkflowVersion :one
SELECT wv.*
FROM workflow_versions wv
JOIN workflows w ON w.id = wv.workflow_id
WHERE wv.id = sqlc.arg(id)
  AND w.owner_id = sqlc.arg(owner_id)
  AND w.deleted_at IS NULL;

-- name: GetLatestWorkflowVersion :one
SELECT wv.*
FROM workflow_versions wv
JOIN workflows w ON w.id = wv.workflow_id
WHERE wv.workflow_id = sqlc.arg(workflow_id)
  AND w.owner_id = sqlc.arg(owner_id)
  AND w.deleted_at IS NULL
ORDER BY wv.version DESC
LIMIT 1;

-- name: TryCreateIdempotencyKey :one
INSERT INTO idempotency_keys (
    owner_id,
    scope,
    key,
    request_sha256,
    locked_until,
    expires_at
)
VALUES (
    sqlc.arg(owner_id),
    sqlc.arg(scope),
    sqlc.arg(key),
    sqlc.arg(request_sha256),
    sqlc.arg(locked_until),
    sqlc.arg(expires_at)
)
ON CONFLICT (owner_id, scope, key) DO NOTHING
RETURNING id;

-- name: LockIdempotencyKey :one
SELECT *
FROM idempotency_keys
WHERE owner_id = sqlc.arg(owner_id)
  AND scope = sqlc.arg(scope)
  AND key = sqlc.arg(key)
FOR UPDATE;

-- name: CompleteIdempotencyKey :execrows
UPDATE idempotency_keys
SET response_status = sqlc.arg(response_status),
    response_body = sqlc.arg(response_body),
    resource_id = sqlc.arg(resource_id),
    locked_until = now()
WHERE id = sqlc.arg(id)
  AND resource_id IS NULL;

-- name: CreateWorkflowRun :one
INSERT INTO workflow_runs (
    workflow_version_id,
    owner_id,
    input,
    cost_budget_microusd,
    deadline_at
)
VALUES (
    sqlc.arg(workflow_version_id),
    sqlc.arg(owner_id),
    sqlc.arg(input),
    sqlc.narg(cost_budget_microusd),
    sqlc.narg(deadline_at)
)
RETURNING *;

-- name: CreateNodeRun :one
INSERT INTO node_runs (run_id, node_key, node_type, dependency_count, status)
VALUES (
    sqlc.arg(run_id),
    sqlc.arg(node_key),
    sqlc.arg(node_type),
    sqlc.arg(dependency_count),
    CASE WHEN sqlc.arg(dependency_count)::integer = 0 THEN 'ready' ELSE 'pending' END
)
RETURNING *;

-- name: GetRun :one
SELECT *
FROM workflow_runs
WHERE id = sqlc.arg(id)
  AND owner_id = sqlc.arg(owner_id);

-- name: GetRunByID :one
SELECT *
FROM workflow_runs
WHERE id = sqlc.arg(id);

-- name: ListRuns :many
SELECT *
FROM workflow_runs
WHERE owner_id = sqlc.arg(owner_id)
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status))
  AND (sqlc.narg(before_created_at)::timestamptz IS NULL OR (created_at, id) < (sqlc.narg(before_created_at), sqlc.narg(before_id)::uuid))
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_size);

-- name: ListRunNodes :many
SELECT nr.*
FROM node_runs nr
JOIN workflow_runs wr ON wr.id = nr.run_id
WHERE nr.run_id = sqlc.arg(run_id)
  AND wr.owner_id = sqlc.arg(owner_id)
ORDER BY nr.created_at, nr.node_key;

-- name: ListRunUsage :many
SELECT ur.*
FROM usage_records ur
JOIN workflow_runs wr ON wr.id = ur.run_id
WHERE ur.run_id = sqlc.arg(run_id)
  AND wr.owner_id = sqlc.arg(owner_id)
ORDER BY ur.recorded_at, ur.id;

-- name: CreateUsageRecord :one
INSERT INTO usage_records (
    run_id,
    node_attempt_id,
    provider,
    model,
    pricing_version,
    input_tokens,
    output_tokens,
    cost_microusd,
    metadata
)
VALUES (
    sqlc.arg(run_id),
    sqlc.arg(node_attempt_id),
    sqlc.arg(provider),
    sqlc.arg(model),
    sqlc.narg(pricing_version),
    sqlc.narg(input_tokens),
    sqlc.narg(output_tokens),
    sqlc.narg(cost_microusd),
    sqlc.arg(metadata)
)
RETURNING *;

-- name: RequestRunCancellation :one
UPDATE workflow_runs
SET status = 'cancelled',
    cancel_requested_at = COALESCE(cancel_requested_at, now()),
    completed_at = COALESCE(completed_at, now()),
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND owner_id = sqlc.arg(owner_id)
  AND status IN ('pending', 'running')
RETURNING *;

-- name: CancelInactiveNodes :execrows
UPDATE node_runs
SET status = 'cancelled',
    completed_at = now(),
    updated_at = now()
WHERE run_id = sqlc.arg(run_id)
  AND status IN ('pending', 'ready', 'queued');

-- name: AllocateEventSequence :one
UPDATE workflow_runs
SET next_event_sequence = next_event_sequence + 1,
    updated_at = now()
WHERE id = sqlc.arg(run_id)
RETURNING (next_event_sequence - 1)::bigint AS sequence;

-- name: AppendExecutionEvent :one
INSERT INTO execution_events (run_id, sequence, event_type, node_run_id, attempt_id, payload)
VALUES (
    sqlc.arg(run_id),
    sqlc.arg(sequence),
    sqlc.arg(event_type),
    sqlc.narg(node_run_id),
    sqlc.narg(attempt_id),
    sqlc.arg(payload)
)
RETURNING *;

-- name: ReplayExecutionEvents :many
SELECT *
FROM execution_events
WHERE run_id = sqlc.arg(run_id)
  AND sequence > sqlc.arg(after_sequence)
ORDER BY sequence
LIMIT sqlc.arg(page_size);

-- name: QueueNodeAttempt :one
WITH queued_node AS (
    UPDATE node_runs
    SET status = 'queued',
        current_attempt_no = current_attempt_no + 1,
        next_attempt_at = NULL,
        updated_at = now()
    WHERE node_runs.id = sqlc.arg(node_run_id)
      AND node_runs.status = 'ready'
      AND (node_runs.next_attempt_at IS NULL OR node_runs.next_attempt_at <= now())
    RETURNING node_runs.id, node_runs.current_attempt_no
)
INSERT INTO node_attempts (node_run_id, attempt_no, fencing_token, input)
SELECT id, current_attempt_no, current_attempt_no::bigint, sqlc.arg(input)
FROM queued_node
RETURNING *;

-- name: AcquireSchedulerLock :exec
SELECT pg_advisory_xact_lock(824173921);

-- name: CountActiveNodes :one
SELECT count(*)::integer
FROM node_runs
WHERE status IN ('queued', 'running');

-- name: LockOneReadyNode :one
SELECT
    nr.*,
    wr.input AS run_input,
    wr.status AS run_status,
    wv.definition AS workflow_definition
FROM node_runs nr
JOIN workflow_runs wr ON wr.id = nr.run_id
JOIN workflow_versions wv ON wv.id = wr.workflow_version_id
WHERE nr.status = 'ready'
  AND (nr.next_attempt_at IS NULL OR nr.next_attempt_at <= now())
  AND wr.status IN ('pending', 'running')
  AND wr.cancel_requested_at IS NULL
  AND (wr.deadline_at IS NULL OR wr.deadline_at > now())
  AND (
      SELECT count(*)
      FROM node_runs active
      WHERE active.run_id = nr.run_id
        AND active.status IN ('queued', 'running')
  ) < sqlc.arg(run_concurrency)::integer
ORDER BY wr.created_at, nr.created_at, nr.id
LIMIT 1
FOR UPDATE OF nr SKIP LOCKED;

-- name: MarkRunRunning :execrows
UPDATE workflow_runs
SET status = 'running',
    started_at = COALESCE(started_at, now()),
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND status = 'pending';

-- name: ListNodeOutputsForRun :many
SELECT node_key, output
FROM node_runs
WHERE run_id = sqlc.arg(run_id)
  AND status = 'succeeded';

-- name: CreateOutboxEvent :one
INSERT INTO outbox_events (aggregate_type, aggregate_id, topic, deduplication_id, payload)
VALUES (
    sqlc.arg(aggregate_type),
    sqlc.arg(aggregate_id),
    sqlc.arg(topic),
    sqlc.arg(deduplication_id),
    sqlc.arg(payload)
)
RETURNING *;

-- name: LockPendingOutboxEvents :many
SELECT *
FROM outbox_events
WHERE published_at IS NULL
  AND available_at <= now()
ORDER BY available_at, created_at, id
LIMIT sqlc.arg(batch_size)
FOR UPDATE SKIP LOCKED;

-- name: MarkOutboxPublished :execrows
UPDATE outbox_events
SET published_at = now(),
    publish_attempts = publish_attempts + 1,
    last_error = NULL
WHERE id = sqlc.arg(id)
  AND published_at IS NULL;

-- name: RecordOutboxFailure :execrows
UPDATE outbox_events
SET publish_attempts = publish_attempts + 1,
    last_error = left(sqlc.arg(last_error), 2000),
    available_at = sqlc.arg(available_at)
WHERE id = sqlc.arg(id)
  AND published_at IS NULL;

-- name: ClaimNodeAttempt :one
UPDATE node_attempts
SET status = 'running',
    worker_id = sqlc.arg(worker_id),
    fencing_token = fencing_token + 1,
    lease_expires_at = sqlc.arg(lease_expires_at),
    last_heartbeat_at = now(),
    started_at = COALESCE(started_at, now())
WHERE node_attempts.id = sqlc.arg(id)
  AND node_attempts.status = 'queued'
  AND EXISTS (
      SELECT 1
      FROM node_runs nr
      JOIN workflow_runs wr ON wr.id = nr.run_id
      WHERE nr.id = node_attempts.node_run_id
        AND nr.status = 'queued'
        AND wr.status IN ('pending', 'running')
        AND wr.cancel_requested_at IS NULL
        AND (wr.deadline_at IS NULL OR wr.deadline_at > now())
  )
RETURNING *;

-- name: MarkNodeRunning :execrows
UPDATE node_runs
SET status = 'running',
    started_at = COALESCE(started_at, now()),
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND status = 'queued';

-- name: GetNodeRunByID :one
SELECT *
FROM node_runs
WHERE id = sqlc.arg(id);

-- name: GetWorkflowDefinitionForRun :one
SELECT wv.definition
FROM workflow_runs wr
JOIN workflow_versions wv ON wv.id = wr.workflow_version_id
WHERE wr.id = sqlc.arg(run_id);

-- name: RenewAttemptLease :one
UPDATE node_attempts
SET lease_expires_at = sqlc.arg(lease_expires_at),
    last_heartbeat_at = now()
WHERE node_attempts.id = sqlc.arg(id)
  AND node_attempts.status = 'running'
  AND fencing_token = sqlc.arg(fencing_token)
  AND worker_id = sqlc.arg(worker_id)
  AND lease_expires_at > now()
RETURNING lease_expires_at;

-- name: CompleteNodeAttempt :one
UPDATE node_attempts
SET status = sqlc.arg(status),
    output = sqlc.narg(output),
    error_code = sqlc.narg(error_code),
    error_message = sqlc.narg(error_message),
    retryable = sqlc.narg(retryable),
    completed_at = now(),
    lease_expires_at = NULL
WHERE id = sqlc.arg(id)
  AND status = 'running'
  AND fencing_token = sqlc.arg(fencing_token)
  AND worker_id = sqlc.arg(worker_id)
RETURNING *;

-- name: CompleteNodeRunSuccess :execrows
UPDATE node_runs
SET status = 'succeeded',
    output = sqlc.arg(output),
    error_code = NULL,
    error_message = NULL,
    completed_at = now(),
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND status = 'running';

-- name: CompleteNodeRunFailure :execrows
UPDATE node_runs
SET status = 'failed',
    error_code = sqlc.arg(error_code),
    error_message = left(sqlc.arg(error_message), 4000),
    completed_at = now(),
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND status = 'running';

-- name: CancelRunningNode :execrows
UPDATE node_runs
SET status = 'cancelled',
    error_code = 'run_cancelled',
    error_message = 'run was cancelled while the node was executing',
    completed_at = now(),
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND status = 'running';

-- name: MarkDependencySucceeded :execrows
UPDATE node_runs
SET succeeded_dependency_count = succeeded_dependency_count + 1,
    status = CASE
        WHEN succeeded_dependency_count + 1 = dependency_count THEN 'ready'
        ELSE status
    END,
    updated_at = now()
WHERE run_id = sqlc.arg(run_id)
  AND node_key = sqlc.arg(node_key)
  AND status = 'pending'
  AND succeeded_dependency_count < dependency_count;

-- name: CountNonTerminalNodes :one
SELECT count(*)::integer
FROM node_runs
WHERE run_id = sqlc.arg(run_id)
  AND status NOT IN ('succeeded', 'failed', 'cancelled', 'skipped');

-- name: CompleteRunSuccess :execrows
UPDATE workflow_runs
SET status = 'succeeded',
    output = sqlc.arg(output),
    completed_at = now(),
    updated_at = now()
WHERE workflow_runs.id = sqlc.arg(id)
  AND workflow_runs.status = 'running'
  AND NOT EXISTS (
      SELECT 1 FROM node_runs
      WHERE run_id = workflow_runs.id
        AND status <> 'succeeded'
  );

-- name: CompleteRunFailure :execrows
UPDATE workflow_runs
SET status = 'failed',
    error_code = sqlc.arg(error_code),
    error_message = left(sqlc.arg(error_message), 4000),
    completed_at = now(),
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND status IN ('pending', 'running');

-- name: SkipUnstartedNodes :execrows
UPDATE node_runs
SET status = 'skipped',
    error_code = 'upstream_failed',
    error_message = 'not executed because a required node failed',
    completed_at = now(),
    updated_at = now()
WHERE run_id = sqlc.arg(run_id)
  AND status IN ('pending', 'ready', 'queued');

-- name: LockExpiredAttempts :many
SELECT *
FROM node_attempts
WHERE status = 'running'
  AND lease_expires_at <= now()
ORDER BY lease_expires_at, id
LIMIT sqlc.arg(batch_size)
FOR UPDATE SKIP LOCKED;

-- name: ExpireAttempt :execrows
UPDATE node_attempts
SET status = 'expired',
    retryable = true,
    error_code = 'lease_expired',
    error_message = 'worker lease expired before completion',
    completed_at = now(),
    lease_expires_at = NULL
WHERE id = sqlc.arg(id)
  AND status = 'running'
  AND fencing_token = sqlc.arg(fencing_token);

-- name: ScheduleNodeRetry :execrows
UPDATE node_runs
SET status = 'ready',
    next_attempt_at = sqlc.arg(next_attempt_at),
    error_code = NULL,
    error_message = NULL,
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND status = 'running';

