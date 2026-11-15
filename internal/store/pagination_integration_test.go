package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jonxsong/Relay/internal/domain"
)

func TestIntegrationPaginatesWorkflowsAndRunsWithoutOverlap(t *testing.T) {
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
	user, err := database.EnsureDevelopmentUser(ctx, "pagination-"+suffix+"@relay.local", "Pagination Test")
	if err != nil {
		t.Fatal(err)
	}
	cleanupIntegrationOwner(t, database, user.ID)
	var workflowID uuid.UUID
	for index := 0; index < 3; index++ {
		created, err := database.CreateWorkflow(ctx, user.ID, fmt.Sprintf("paged-%d-%s", index, suffix), "pagination", domain.WorkflowDefinition{
			Nodes: []domain.NodeDefinition{{ID: "work", Type: domain.NodeTypeTransform}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			workflowID = created.Workflow.ID
		}
	}
	firstWorkflows, err := database.ListWorkflows(ctx, user.ID, nil, 2)
	if err != nil || len(firstWorkflows.Items) != 2 || firstWorkflows.NextCursor == nil {
		t.Fatalf("first workflow page = %+v, err=%v", firstWorkflows, err)
	}
	secondWorkflows, err := database.ListWorkflows(ctx, user.ID, firstWorkflows.NextCursor, 2)
	if err != nil || len(secondWorkflows.Items) != 1 || secondWorkflows.NextCursor != nil {
		t.Fatalf("second workflow page = %+v, err=%v", secondWorkflows, err)
	}
	if firstWorkflows.Items[0].ID == secondWorkflows.Items[0].ID || firstWorkflows.Items[1].ID == secondWorkflows.Items[0].ID {
		t.Fatal("workflow cursor returned a duplicate item")
	}

	for index := 0; index < 3; index++ {
		if _, err := database.SubmitRun(ctx, SubmitRunParams{
			OwnerID: user.ID, WorkflowID: workflowID, IdempotencyKey: fmt.Sprintf("page-%d-%s", index, suffix), Input: json.RawMessage(`{}`),
		}); err != nil {
			t.Fatal(err)
		}
	}
	firstRuns, err := database.ListRuns(ctx, user.ID, nil, nil, 2)
	if err != nil || len(firstRuns.Items) != 2 || firstRuns.NextCursor == nil {
		t.Fatalf("first run page = %+v, err=%v", firstRuns, err)
	}
	secondRuns, err := database.ListRuns(ctx, user.ID, nil, firstRuns.NextCursor, 2)
	if err != nil || len(secondRuns.Items) != 1 || secondRuns.NextCursor != nil {
		t.Fatalf("second run page = %+v, err=%v", secondRuns, err)
	}
	if firstRuns.Items[0].ID == secondRuns.Items[0].ID || firstRuns.Items[1].ID == secondRuns.Items[0].ID {
		t.Fatal("run cursor returned a duplicate item")
	}
}
