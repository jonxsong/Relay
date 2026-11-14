-- +goose Up
-- +goose StatementBegin

CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email text NOT NULL,
    display_name text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT users_email_unique UNIQUE (email),
    CONSTRAINT users_email_bounded CHECK (length(email) BETWEEN 3 AND 320)
);

CREATE TABLE workflows (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    CONSTRAINT workflows_owner_name_unique UNIQUE NULLS NOT DISTINCT (owner_id, name, deleted_at),
    CONSTRAINT workflows_name_bounded CHECK (length(name) BETWEEN 1 AND 160),
    CONSTRAINT workflows_description_bounded CHECK (length(description) <= 4000)
);

CREATE TABLE workflow_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id uuid NOT NULL REFERENCES workflows(id) ON DELETE RESTRICT,
    version integer NOT NULL,
    definition jsonb NOT NULL,
    definition_sha256 char(64) NOT NULL,
    created_by uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT workflow_versions_number_unique UNIQUE (workflow_id, version),
    CONSTRAINT workflow_versions_hash_unique UNIQUE (workflow_id, definition_sha256),
    CONSTRAINT workflow_versions_positive CHECK (version > 0),
    CONSTRAINT workflow_versions_definition_object CHECK (jsonb_typeof(definition) = 'object')
);

CREATE TABLE workflow_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_version_id uuid NOT NULL REFERENCES workflow_versions(id) ON DELETE RESTRICT,
    owner_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    status text NOT NULL DEFAULT 'pending',
    input jsonb NOT NULL DEFAULT '{}'::jsonb,
    output jsonb,
    error_code text,
    error_message text,
    cost_budget_microusd bigint,
    deadline_at timestamptz,
    cancel_requested_at timestamptz,
    started_at timestamptz,
    completed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT workflow_runs_status_valid CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'cancelled')),
    CONSTRAINT workflow_runs_input_object CHECK (jsonb_typeof(input) = 'object'),
    CONSTRAINT workflow_runs_budget_nonnegative CHECK (cost_budget_microusd IS NULL OR cost_budget_microusd >= 0),
    CONSTRAINT workflow_runs_terminal_time CHECK ((status IN ('succeeded', 'failed', 'cancelled')) = (completed_at IS NOT NULL))
);

CREATE INDEX workflow_runs_owner_created_idx ON workflow_runs (owner_id, created_at DESC, id DESC);
CREATE INDEX workflow_runs_active_idx ON workflow_runs (created_at, id) WHERE status IN ('pending', 'running');

CREATE TABLE node_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id uuid NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
    node_key text NOT NULL,
    node_type text NOT NULL,
    status text NOT NULL DEFAULT 'pending',
    dependency_count integer NOT NULL DEFAULT 0,
    succeeded_dependency_count integer NOT NULL DEFAULT 0,
    current_attempt_no integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz,
    output jsonb,
    error_code text,
    error_message text,
    started_at timestamptz,
    completed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT node_runs_key_unique UNIQUE (run_id, node_key),
    CONSTRAINT node_runs_key_bounded CHECK (length(node_key) BETWEEN 1 AND 128),
    CONSTRAINT node_runs_type_valid CHECK (node_type IN ('llm', 'tool', 'transform', 'join')),
    CONSTRAINT node_runs_status_valid CHECK (status IN ('pending', 'ready', 'queued', 'running', 'succeeded', 'failed', 'cancelled', 'skipped')),
    CONSTRAINT node_runs_dependency_counts_valid CHECK (dependency_count >= 0 AND succeeded_dependency_count BETWEEN 0 AND dependency_count),
    CONSTRAINT node_runs_attempt_nonnegative CHECK (current_attempt_no >= 0)
);

CREATE INDEX node_runs_runnable_idx ON node_runs (next_attempt_at, run_id, id) WHERE status = 'ready';
CREATE INDEX node_runs_run_status_idx ON node_runs (run_id, status, id);

CREATE TABLE node_attempts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    node_run_id uuid NOT NULL REFERENCES node_runs(id) ON DELETE CASCADE,
    attempt_no integer NOT NULL,
    status text NOT NULL DEFAULT 'queued',
    worker_id text,
    fencing_token bigint NOT NULL,
    lease_expires_at timestamptz,
    last_heartbeat_at timestamptz,
    input jsonb,
    output jsonb,
    error_code text,
    error_message text,
    retryable boolean,
    queued_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    completed_at timestamptz,
    CONSTRAINT node_attempts_number_unique UNIQUE (node_run_id, attempt_no),
    CONSTRAINT node_attempts_number_positive CHECK (attempt_no > 0),
    CONSTRAINT node_attempts_fence_positive CHECK (fencing_token > 0),
    CONSTRAINT node_attempts_status_valid CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled', 'expired')),
    CONSTRAINT node_attempts_running_lease CHECK (status <> 'running' OR (worker_id IS NOT NULL AND lease_expires_at IS NOT NULL))
);

CREATE INDEX node_attempts_expired_lease_idx ON node_attempts (lease_expires_at, id) WHERE status = 'running';
CREATE INDEX node_attempts_node_status_idx ON node_attempts (node_run_id, status, attempt_no DESC);

CREATE TABLE execution_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id uuid NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
    sequence bigint NOT NULL,
    event_type text NOT NULL,
    node_run_id uuid REFERENCES node_runs(id) ON DELETE CASCADE,
    attempt_id uuid REFERENCES node_attempts(id) ON DELETE CASCADE,
    payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    occurred_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT execution_events_sequence_unique UNIQUE (run_id, sequence),
    CONSTRAINT execution_events_sequence_positive CHECK (sequence > 0),
    CONSTRAINT execution_events_type_bounded CHECK (length(event_type) BETWEEN 1 AND 100),
    CONSTRAINT execution_events_payload_object CHECK (jsonb_typeof(payload) = 'object')
);

CREATE INDEX execution_events_replay_idx ON execution_events (run_id, sequence);

CREATE TABLE outbox_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    aggregate_type text NOT NULL,
    aggregate_id uuid NOT NULL,
    topic text NOT NULL,
    deduplication_id text NOT NULL,
    payload jsonb NOT NULL,
    available_at timestamptz NOT NULL DEFAULT now(),
    publish_attempts integer NOT NULL DEFAULT 0,
    last_error text,
    published_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT outbox_events_dedup_unique UNIQUE (deduplication_id),
    CONSTRAINT outbox_events_attempts_nonnegative CHECK (publish_attempts >= 0),
    CONSTRAINT outbox_events_payload_object CHECK (jsonb_typeof(payload) = 'object')
);

CREATE INDEX outbox_events_pending_idx ON outbox_events (available_at, created_at, id) WHERE published_at IS NULL;

CREATE TABLE tool_connections (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name text NOT NULL,
    endpoint text NOT NULL,
    secret_ref text,
    allowed_tools jsonb NOT NULL DEFAULT '[]'::jsonb,
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT tool_connections_owner_name_unique UNIQUE (owner_id, name),
    CONSTRAINT tool_connections_allowed_array CHECK (jsonb_typeof(allowed_tools) = 'array'),
    CONSTRAINT tool_connections_no_inline_secret CHECK (endpoint !~* '(token|key|secret|password)=')
);

CREATE TABLE usage_records (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id uuid NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
    node_attempt_id uuid REFERENCES node_attempts(id) ON DELETE SET NULL,
    provider text NOT NULL,
    model text NOT NULL,
    pricing_version text,
    input_tokens bigint,
    output_tokens bigint,
    cost_microusd bigint,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    recorded_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT usage_records_tokens_nonnegative CHECK ((input_tokens IS NULL OR input_tokens >= 0) AND (output_tokens IS NULL OR output_tokens >= 0)),
    CONSTRAINT usage_records_cost_nonnegative CHECK (cost_microusd IS NULL OR cost_microusd >= 0),
    CONSTRAINT usage_records_metadata_object CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE INDEX usage_records_run_idx ON usage_records (run_id, recorded_at, id);

CREATE TABLE api_keys (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name text NOT NULL,
    key_prefix text NOT NULL,
    key_hash bytea NOT NULL,
    scopes text[] NOT NULL DEFAULT '{}',
    expires_at timestamptz,
    last_used_at timestamptz,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT api_keys_owner_name_unique UNIQUE (owner_id, name),
    CONSTRAINT api_keys_hash_unique UNIQUE (key_hash),
    CONSTRAINT api_keys_prefix_bounded CHECK (length(key_prefix) BETWEEN 4 AND 24),
    CONSTRAINT api_keys_hash_nonempty CHECK (octet_length(key_hash) >= 32)
);

CREATE INDEX api_keys_active_prefix_idx ON api_keys (key_prefix) WHERE revoked_at IS NULL;

CREATE TABLE idempotency_keys (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    scope text NOT NULL,
    key text NOT NULL,
    request_sha256 char(64) NOT NULL,
    response_status integer,
    response_body jsonb,
    resource_id uuid,
    locked_until timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT idempotency_keys_owner_scope_key_unique UNIQUE (owner_id, scope, key),
    CONSTRAINT idempotency_keys_key_bounded CHECK (length(key) BETWEEN 1 AND 255),
    CONSTRAINT idempotency_keys_scope_bounded CHECK (length(scope) BETWEEN 1 AND 100),
    CONSTRAINT idempotency_keys_response_status_valid CHECK (response_status IS NULL OR response_status BETWEEN 100 AND 599),
    CONSTRAINT idempotency_keys_expiry_valid CHECK (expires_at > created_at)
);

CREATE INDEX idempotency_keys_expiry_idx ON idempotency_keys (expires_at);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS idempotency_keys;
DROP TABLE IF EXISTS api_keys;
DROP TABLE IF EXISTS usage_records;
DROP TABLE IF EXISTS tool_connections;
DROP TABLE IF EXISTS outbox_events;
DROP TABLE IF EXISTS execution_events;
DROP TABLE IF EXISTS node_attempts;
DROP TABLE IF EXISTS node_runs;
DROP TABLE IF EXISTS workflow_runs;
DROP TABLE IF EXISTS workflow_versions;
DROP TABLE IF EXISTS workflows;
DROP TABLE IF EXISTS users;
-- +goose StatementEnd

