//go:build integration

package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jonxsong/Relay/internal/domain"
	"github.com/jonxsong/Relay/internal/store/db"
)

func TestIntegrationRecoversCrashWindowAndRejectsStaleWorkers(t *testing.T) {
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
	for {
		drained, err := database.DispatchOutbox(ctx, 1000, func(context.Context, db.OutboxEvent) error { return nil })
		if err != nil {
			t.Fatalf("drain prior test outbox: %v", err)
		}
		if drained == 0 {
			break
		}
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user, err := database.EnsureDevelopmentUser(ctx, "durability-"+suffix+"@relay.local", "Durability Test")
	if err != nil {
		t.Fatal(err)
	}
	workflow, err := database.CreateWorkflow(ctx, user.ID, "durability-"+suffix, "lease and outbox recovery", domain.WorkflowDefinition{Nodes: []domain.NodeDefinition{{
		ID: "work", Type: domain.NodeTypeLLM, Inputs: map[string]domain.InputBinding{"prompt": {From: "$.input.prompt"}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	submitted, err := database.SubmitRun(ctx, SubmitRunParams{
		OwnerID: user.ID, WorkflowID: workflow.Workflow.ID, IdempotencyKey: "durability-" + suffix, Input: json.RawMessage(`{"prompt":"recover me"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.pool.Exec(ctx, `UPDATE workflow_runs SET created_at = '1999-12-31T00:00:00Z' WHERE id = $1`, submitted.Run.ID); err != nil {
		t.Fatal(err)
	}
	first, found, err := database.ScheduleOne(ctx, 10000, 10000)
	if err != nil || !found {
		t.Fatalf("ScheduleOne() = (%+v, %v, %v), want scheduled attempt", first, found, err)
	}

	publishCalls := 0
	crashed := false
	func() {
		defer func() {
			if recover() != nil {
				crashed = true
			}
		}()
		_, _ = database.DispatchOutbox(ctx, 100, func(context.Context, db.OutboxEvent) error {
			publishCalls++
			panic("simulated process crash after broker publish")
		})
	}()
	if !crashed || publishCalls != 1 {
		t.Fatalf("crash simulation = %v with %d publishes, want true and 1", crashed, publishCalls)
	}
	published, err := database.DispatchOutbox(ctx, 100, func(context.Context, db.OutboxEvent) error {
		publishCalls++
		return nil
	})
	if err != nil || published != 1 || publishCalls != 2 {
		t.Fatalf("redelivery dispatch = (%d, %v), publish calls %d; want (1, nil), 2", published, err, publishCalls)
	}

	staleClaim, err := database.ClaimAttempt(ctx, first.AttemptID, "worker-before-crash", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.pool.Exec(ctx, `UPDATE node_attempts SET lease_expires_at = now() - interval '1 second' WHERE id = $1`, first.AttemptID); err != nil {
		t.Fatal(err)
	}
	recovered, err := database.RecoverExpiredAttempts(ctx, 100, 2)
	if err != nil || recovered != 1 {
		t.Fatalf("RecoverExpiredAttempts() = (%d, %v), want (1, nil)", recovered, err)
	}
	if err := database.CompleteAttemptSuccess(ctx, staleClaim, json.RawMessage(`{"late":true}`), nil); !errors.Is(err, ErrStaleClaim) {
		t.Fatalf("stale completion error = %v, want ErrStaleClaim", err)
	}
	if _, err := database.pool.Exec(ctx, `UPDATE node_runs SET next_attempt_at = now() WHERE id = $1`, first.NodeRunID); err != nil {
		t.Fatal(err)
	}
	second, found, err := database.ScheduleOne(ctx, 10000, 10000)
	if err != nil || !found || second.AttemptID == first.AttemptID {
		t.Fatalf("retry ScheduleOne() = (%+v, %v, %v)", second, found, err)
	}

	type claimResult struct {
		claim AttemptClaim
		err   error
	}
	results := make(chan claimResult, 2)
	var workers sync.WaitGroup
	for _, workerID := range []string{"duplicate-a", "duplicate-b"} {
		workers.Add(1)
		go func(id string) {
			defer workers.Done()
			claim, claimErr := database.ClaimAttempt(ctx, second.AttemptID, id, 5*time.Second)
			results <- claimResult{claim: claim, err: claimErr}
		}(workerID)
	}
	workers.Wait()
	close(results)
	var winner AttemptClaim
	successes, stale := 0, 0
	for result := range results {
		switch {
		case result.err == nil:
			successes++
			winner = result.claim
		case errors.Is(result.err, ErrStaleClaim):
			stale++
		default:
			t.Fatalf("duplicate claim error = %v", result.err)
		}
	}
	if successes != 1 || stale != 1 {
		t.Fatalf("duplicate claim results = %d success, %d stale; want 1 and 1", successes, stale)
	}
	if err := database.CompleteAttemptSuccess(ctx, winner, json.RawMessage(`{"recovered":true}`), nil); err != nil {
		t.Fatal(err)
	}
	run, err := database.GetRun(ctx, user.ID, submitted.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != string(domain.RunSucceeded) {
		t.Fatalf("run status = %q, want succeeded", run.Status)
	}
	attempts, err := database.ListNodeAttempts(ctx, user.ID, run.ID, second.NodeRunID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 2 || attempts[0].Status != "expired" || attempts[1].Status != "succeeded" {
		t.Fatalf("attempt statuses = %+v, want expired then succeeded", attempts)
	}
}
