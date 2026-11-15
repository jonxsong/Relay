package store

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// cleanupIntegrationOwner keeps stateful integration fixtures from leaking into
// later tests that intentionally reuse the same PostgreSQL service. The cleanup
// is owner-scoped so running the suite against a shared development database
// cannot remove unrelated data.
func cleanupIntegrationOwner(t *testing.T, database *Store, ownerID uuid.UUID) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		tx, err := database.pool.Begin(ctx)
		if err != nil {
			t.Errorf("begin integration fixture cleanup: %v", err)
			return
		}
		defer func() { _ = tx.Rollback(ctx) }()

		statements := []string{
			`DELETE FROM outbox_events
			 WHERE aggregate_type = 'node_attempt'
			   AND aggregate_id IN (
			       SELECT attempt.id
			       FROM node_attempts AS attempt
			       JOIN node_runs AS node ON node.id = attempt.node_run_id
			       JOIN workflow_runs AS run ON run.id = node.run_id
			       WHERE run.owner_id = $1
			   )`,
			`DELETE FROM workflow_runs WHERE owner_id = $1`,
			`DELETE FROM workflow_versions
			 WHERE workflow_id IN (SELECT id FROM workflows WHERE owner_id = $1)`,
			`DELETE FROM workflows WHERE owner_id = $1`,
			`DELETE FROM users WHERE id = $1`,
		}
		for _, statement := range statements {
			if _, err := tx.Exec(ctx, statement, ownerID); err != nil {
				t.Errorf("clean integration fixture: %v", err)
				return
			}
		}
		if err := tx.Commit(ctx); err != nil {
			t.Errorf("commit integration fixture cleanup: %v", err)
		}
	})
}
