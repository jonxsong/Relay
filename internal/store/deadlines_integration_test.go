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

func TestIntegrationExpiresRunDeadlineAndFencesWorker(t *testing.T) {
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
	defer database.Close()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user, err := database.EnsureDevelopmentUser(ctx, "deadline-"+suffix+"@relay.local", "Deadline Test")
	if err != nil {
		t.Fatal(err)
	}
	workflow, err := database.CreateWorkflow(ctx, user.ID, "deadline-"+suffix, "deadline fencing", domain.WorkflowDefinition{
		Name: "deadline fencing",
		Nodes: []domain.NodeDefinition{{
			ID: "work", Type: domain.NodeTypeLLM, TimeoutSeconds: 1,
			Inputs: map[string]domain.InputBinding{"prompt": {From: "$.input.prompt"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().UTC().Add(time.Minute)
	submitted, err := database.SubmitRun(ctx, SubmitRunParams{
		OwnerID: user.ID, WorkflowID: workflow.Workflow.ID, IdempotencyKey: "deadline-" + suffix,
		Input: json.RawMessage(`{"prompt":"stop on time"}`), Deadline: &deadline,
	})
	if err != nil {
		t.Fatal(err)
	}
	scheduled, found, err := database.ScheduleOne(ctx, 10, 10)
	if err != nil || !found {
		t.Fatalf("ScheduleOne() = (%+v, %v, %v), want scheduled attempt", scheduled, found, err)
	}
	claim, err := database.ClaimAttempt(ctx, scheduled.AttemptID, "deadline-worker", 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if claim.NodeTimeout != time.Second || claim.RunDeadline == nil {
		t.Fatalf("claim deadlines = node %s, run %v", claim.NodeTimeout, claim.RunDeadline)
	}
	if _, err := database.pool.Exec(ctx, `UPDATE workflow_runs SET deadline_at = now() - interval '1 second' WHERE id = $1`, submitted.Run.ID); err != nil {
		t.Fatal(err)
	}
	expired, err := database.ExpireOverdueRuns(ctx, 100)
	if err != nil || expired != 1 {
		t.Fatalf("ExpireOverdueRuns() = (%d, %v), want (1, nil)", expired, err)
	}
	if err := database.CompleteAttemptSuccess(ctx, claim, json.RawMessage(`{"late":true}`), nil); !errors.Is(err, ErrStaleClaim) {
		t.Fatalf("late completion error = %v, want ErrStaleClaim", err)
	}
	run, err := database.GetRun(ctx, user.ID, submitted.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != string(domain.RunFailed) || run.ErrorCode == nil || *run.ErrorCode != "run_deadline_exceeded" {
		t.Fatalf("run = status %q code %v, want failed/run_deadline_exceeded", run.Status, run.ErrorCode)
	}
	events, err := database.ReplayEvents(ctx, user.ID, run.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 || events[len(events)-1].EventType != "run.failed" {
		t.Fatalf("last event = %+v, want run.failed", events)
	}
}
