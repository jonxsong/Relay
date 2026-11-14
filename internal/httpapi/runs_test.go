package httpapi

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jonxsong/Relay/internal/domain"
	"github.com/jonxsong/Relay/internal/store"
	"github.com/jonxsong/Relay/internal/store/db"
)

type fakeRuns struct {
	submit   func(context.Context, store.SubmitRunParams) (store.SubmittedRun, error)
	get      func(context.Context, uuid.UUID, uuid.UUID) (db.WorkflowRun, error)
	attempts func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) ([]db.NodeAttempt, error)
}

func (f fakeRuns) SubmitRun(ctx context.Context, params store.SubmitRunParams) (store.SubmittedRun, error) {
	if f.submit == nil {
		return store.SubmittedRun{}, errors.New("unexpected SubmitRun call")
	}
	return f.submit(ctx, params)
}

func (f fakeRuns) GetRun(ctx context.Context, ownerID, runID uuid.UUID) (db.WorkflowRun, error) {
	if f.get != nil {
		return f.get(ctx, ownerID, runID)
	}
	return db.WorkflowRun{}, store.ErrNotFound
}
func (fakeRuns) ListRuns(context.Context, uuid.UUID, *string, int32) ([]db.WorkflowRun, error) {
	return []db.WorkflowRun{}, nil
}
func (fakeRuns) ListRunNodes(context.Context, uuid.UUID, uuid.UUID) ([]db.NodeRun, error) {
	return []db.NodeRun{}, nil
}

func (f fakeRuns) ListNodeAttempts(ctx context.Context, ownerID, runID, nodeRunID uuid.UUID) ([]db.NodeAttempt, error) {
	if f.attempts != nil {
		return f.attempts(ctx, ownerID, runID, nodeRunID)
	}
	return []db.NodeAttempt{}, nil
}
func (fakeRuns) ReplayEvents(context.Context, uuid.UUID, uuid.UUID, int64, int32) ([]db.ExecutionEvent, error) {
	return []db.ExecutionEvent{}, nil
}
func (fakeRuns) ListRunUsage(context.Context, uuid.UUID, uuid.UUID) ([]db.UsageRecord, error) {
	return []db.UsageRecord{}, nil
}
func (fakeRuns) CancelRun(context.Context, uuid.UUID, uuid.UUID) (db.WorkflowRun, error) {
	return db.WorkflowRun{}, store.ErrNotFound
}
func (fakeRuns) GetRunDefinition(context.Context, uuid.UUID, uuid.UUID) (domain.WorkflowDefinition, error) {
	return domain.WorkflowDefinition{}, store.ErrNotFound
}

func TestSubmitRunRequiresIdempotencyKey(t *testing.T) {
	t.Parallel()
	router := NewRouter(Dependencies{
		Readiness: fakeReadiness{}, Workflows: fakeWorkflows{}, Runs: fakeRuns{}, DevelopmentUser: uuid.New(), MaxRequestBytes: 4096,
	})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/"+uuid.NewString()+"/runs", bytes.NewBufferString(`{"input":{}}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusBadRequest, response.Body.String())
	}
}

func TestSubmitRunReturnsAcceptedAndReplayHeader(t *testing.T) {
	t.Parallel()
	ownerID := uuid.New()
	workflowID := uuid.New()
	runID := uuid.New()
	runs := fakeRuns{submit: func(_ context.Context, params store.SubmitRunParams) (store.SubmittedRun, error) {
		if params.OwnerID != ownerID || params.WorkflowID != workflowID || params.IdempotencyKey != "request-1" {
			t.Fatalf("unexpected submit params: %+v", params)
		}
		return store.SubmittedRun{Run: db.WorkflowRun{ID: runID, WorkflowVersionID: uuid.New(), Status: "pending", Input: []byte(`{}`)}, Existing: true}, nil
	}}
	router := NewRouter(Dependencies{
		Readiness: fakeReadiness{}, Workflows: fakeWorkflows{}, Runs: runs, DevelopmentUser: ownerID, MaxRequestBytes: 4096,
	})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/"+workflowID.String()+"/runs", bytes.NewBufferString(`{"input":{}}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "request-1")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusAccepted, response.Body.String())
	}
	if response.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatal("missing Idempotency-Replayed header")
	}
}

func TestStreamRejectsInvalidLastEventID(t *testing.T) {
	t.Parallel()
	router := NewRouter(Dependencies{
		Readiness: fakeReadiness{}, Workflows: fakeWorkflows{}, Runs: fakeRuns{}, DevelopmentUser: uuid.New(), MaxRequestBytes: 4096,
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/runs/"+uuid.NewString()+"/stream", nil)
	request.Header.Set("Last-Event-ID", "not-a-sequence")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusBadRequest, response.Body.String())
	}
}

func TestListNodeAttemptsUsesScopedIdentifiers(t *testing.T) {
	t.Parallel()
	ownerID, runID, nodeRunID, attemptID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	runs := fakeRuns{attempts: func(_ context.Context, gotOwner, gotRun, gotNode uuid.UUID) ([]db.NodeAttempt, error) {
		if gotOwner != ownerID || gotRun != runID || gotNode != nodeRunID {
			t.Fatalf("scope = (%s, %s, %s)", gotOwner, gotRun, gotNode)
		}
		return []db.NodeAttempt{{ID: attemptID, NodeRunID: nodeRunID, AttemptNo: 1, Status: "succeeded", FencingToken: 1}}, nil
	}}
	router := NewRouter(Dependencies{Readiness: fakeReadiness{}, Workflows: fakeWorkflows{}, Runs: runs, DevelopmentUser: ownerID, MaxRequestBytes: 4096})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/runs/"+runID.String()+"/nodes/"+nodeRunID.String()+"/attempts", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	if !bytes.Contains(response.Body.Bytes(), []byte(attemptID.String())) {
		t.Fatalf("response does not contain attempt id: %s", response.Body.String())
	}
}
