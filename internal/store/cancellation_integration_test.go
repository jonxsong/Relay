package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jonxsong/Relay/internal/domain"
)

func TestIntegrationCancellationFencesActiveWorker(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	database, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user, err := database.EnsureDevelopmentUser(ctx, "cancel-"+suffix+"@relay.local", "Cancellation Test")
	if err != nil {
		t.Fatal(err)
	}
	cleanupIntegrationOwner(t, database, user.ID)
	workflow, err := database.CreateWorkflow(ctx, user.ID, "cancel-"+suffix, "active cancellation", domain.WorkflowDefinition{
		Name:  "active cancellation",
		Nodes: []domain.NodeDefinition{{ID: "work", Type: domain.NodeTypeLLM}},
	})
	if err != nil {
		t.Fatal(err)
	}
	submitted, err := database.SubmitRun(ctx, SubmitRunParams{
		OwnerID: user.ID, WorkflowID: workflow.Workflow.ID, IdempotencyKey: "cancel-" + suffix, Input: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.pool.Exec(ctx, `UPDATE workflow_runs SET created_at = '2000-01-01T00:00:00Z' WHERE id = $1`, submitted.Run.ID); err != nil {
		t.Fatal(err)
	}
	scheduled, found, err := database.ScheduleOne(ctx, 10000, 10000)
	if err != nil || !found {
		t.Fatalf("ScheduleOne() = (%+v, %v, %v), want scheduled attempt", scheduled, found, err)
	}
	claim, err := database.ClaimAttempt(ctx, scheduled.AttemptID, "cancel-worker", 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CancelRun(ctx, user.ID, submitted.Run.ID); err != nil {
		t.Fatal(err)
	}
	if err := database.RenewLease(ctx, claim, 15*time.Second); !errors.Is(err, ErrStaleClaim) {
		t.Fatalf("renew after cancellation error = %v, want ErrStaleClaim", err)
	}
	if err := database.CompleteAttemptSuccess(ctx, claim, json.RawMessage(`{"late":true}`), nil); !errors.Is(err, ErrRunCancelled) && !errors.Is(err, ErrStaleClaim) {
		t.Fatalf("late completion error = %v, want cancellation or stale claim", err)
	}
	nodes, err := database.ListRunNodes(ctx, user.ID, submitted.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].Status != string(domain.NodeCancelled) {
		t.Fatalf("nodes = %+v, want one cancelled node", nodes)
	}
}
