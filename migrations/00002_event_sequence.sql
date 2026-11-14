-- +goose Up
ALTER TABLE workflow_runs
    ADD COLUMN next_event_sequence bigint NOT NULL DEFAULT 1,
    ADD CONSTRAINT workflow_runs_next_event_sequence_positive CHECK (next_event_sequence > 0);

-- +goose Down
ALTER TABLE workflow_runs
    DROP CONSTRAINT workflow_runs_next_event_sequence_positive,
    DROP COLUMN next_event_sequence;

