package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jonxsong/Relay/internal/domain"
)

func TestIntegrationCostBudgetTerminatesRunAndFencesRemainingWork(t *testing.T) {
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
	user, err := database.EnsureDevelopmentUser(ctx, "budget-"+suffix+"@relay.local", "Budget Test")
	if err != nil {
		t.Fatal(err)
	}
	cleanupIntegrationOwner(t, database, user.ID)
	workflow, err := database.CreateWorkflow(ctx, user.ID, "budget-"+suffix, "cost budget", domain.WorkflowDefinition{
		Name: "cost budget",
		Nodes: []domain.NodeDefinition{
			{ID: "generate", Type: domain.NodeTypeLLM},
			{ID: "summarize", Type: domain.NodeTypeLLM, DependsOn: []string{"generate"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	budget := int64(10)
	submitted, err := database.SubmitRun(ctx, SubmitRunParams{
		OwnerID: user.ID, WorkflowID: workflow.Workflow.ID, IdempotencyKey: "budget-" + suffix,
		Input: json.RawMessage(`{}`), CostBudgetMicrousd: &budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.pool.Exec(ctx, `UPDATE workflow_runs SET created_at = '2000-01-03T00:00:00Z' WHERE id = $1`, submitted.Run.ID); err != nil {
		t.Fatal(err)
	}
	scheduled, found, err := database.ScheduleOne(ctx, 10000, 10000)
	if err != nil || !found || scheduled.RunID != submitted.Run.ID {
		t.Fatalf("ScheduleOne() = (%+v, %v, %v), want budget run", scheduled, found, err)
	}
	claim, err := database.ClaimAttempt(ctx, scheduled.AttemptID, "budget-worker", 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	cost := int64(11)
	if err := database.CompleteAttemptSuccess(ctx, claim, json.RawMessage(`{"text":"done"}`), &domain.Usage{
		Provider: "test", Model: "deterministic", CostMicrousd: &cost,
	}); err != nil {
		t.Fatal(err)
	}
	run, err := database.GetRun(ctx, user.ID, submitted.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != string(domain.RunFailed) || run.ErrorCode == nil || *run.ErrorCode != "cost_budget_exceeded" {
		t.Fatalf("run = status %q code %v, want failed/cost_budget_exceeded", run.Status, run.ErrorCode)
	}
	nodes, err := database.ListRunNodes(ctx, user.ID, submitted.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 || nodes[0].Status != string(domain.NodeSucceeded) || nodes[1].Status != string(domain.NodeFailed) {
		t.Fatalf("node statuses = %+v, want succeeded then failed", nodes)
	}
}
