package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jonxsong/Relay/internal/domain"
	"github.com/jonxsong/Relay/internal/store"
	"github.com/jonxsong/Relay/internal/store/db"
)

type RunRepository interface {
	SubmitRun(context.Context, store.SubmitRunParams) (store.SubmittedRun, error)
	GetRun(context.Context, uuid.UUID, uuid.UUID) (db.WorkflowRun, error)
	ListRuns(context.Context, uuid.UUID, *string, *store.PageCursor, int32) (store.RunPage, error)
	ListRunNodes(context.Context, uuid.UUID, uuid.UUID) ([]db.NodeRun, error)
	ListNodeAttempts(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) ([]db.NodeAttempt, error)
	ReplayEvents(context.Context, uuid.UUID, uuid.UUID, int64, int32) ([]db.ExecutionEvent, error)
	ListRunUsage(context.Context, uuid.UUID, uuid.UUID) ([]db.UsageRecord, error)
	CancelRun(context.Context, uuid.UUID, uuid.UUID) (db.WorkflowRun, error)
	GetRunDefinition(context.Context, uuid.UUID, uuid.UUID) (domain.WorkflowDefinition, error)
}

type submitRunRequest struct {
	Input              json.RawMessage `json:"input"`
	CostBudgetMicrousd *int64          `json:"cost_budget_microusd,omitempty"`
	Deadline           *time.Time      `json:"deadline,omitempty"`
}

type runResponse struct {
	ID                 uuid.UUID       `json:"id"`
	WorkflowVersionID  uuid.UUID       `json:"workflow_version_id"`
	Status             string          `json:"status"`
	Input              json.RawMessage `json:"input"`
	Output             json.RawMessage `json:"output,omitempty"`
	ErrorCode          *string         `json:"error_code,omitempty"`
	ErrorMessage       *string         `json:"error_message,omitempty"`
	CostBudgetMicrousd *int64          `json:"cost_budget_microusd,omitempty"`
	Deadline           *time.Time      `json:"deadline,omitempty"`
	CreatedAt          time.Time       `json:"created_at"`
	StartedAt          *time.Time      `json:"started_at,omitempty"`
	CompletedAt        *time.Time      `json:"completed_at,omitempty"`
}

type nodeRunResponse struct {
	ID               uuid.UUID       `json:"id"`
	NodeKey          string          `json:"node_key"`
	NodeType         string          `json:"node_type"`
	Status           string          `json:"status"`
	CurrentAttemptNo int32           `json:"current_attempt_no"`
	Output           json.RawMessage `json:"output,omitempty"`
	ErrorCode        *string         `json:"error_code,omitempty"`
	ErrorMessage     *string         `json:"error_message,omitempty"`
	DependsOn        []string        `json:"depends_on"`
	TimeoutSeconds   int32           `json:"timeout_seconds"`
}

type eventResponse struct {
	ID         uuid.UUID       `json:"id"`
	Sequence   int64           `json:"sequence"`
	EventType  string          `json:"event_type"`
	NodeRunID  *uuid.UUID      `json:"node_run_id,omitempty"`
	AttemptID  *uuid.UUID      `json:"attempt_id,omitempty"`
	Payload    json.RawMessage `json:"payload"`
	OccurredAt time.Time       `json:"occurred_at"`
}

type nodeAttemptResponse struct {
	ID              uuid.UUID       `json:"id"`
	NodeRunID       uuid.UUID       `json:"node_run_id"`
	AttemptNo       int32           `json:"attempt_no"`
	Status          string          `json:"status"`
	WorkerID        *string         `json:"worker_id,omitempty"`
	FencingToken    int64           `json:"fencing_token"`
	LeaseExpiresAt  *time.Time      `json:"lease_expires_at,omitempty"`
	LastHeartbeatAt *time.Time      `json:"last_heartbeat_at,omitempty"`
	Input           json.RawMessage `json:"input"`
	Output          json.RawMessage `json:"output,omitempty"`
	ErrorCode       *string         `json:"error_code,omitempty"`
	ErrorMessage    *string         `json:"error_message,omitempty"`
	Retryable       *bool           `json:"retryable,omitempty"`
	QueuedAt        time.Time       `json:"queued_at"`
	StartedAt       *time.Time      `json:"started_at,omitempty"`
	CompletedAt     *time.Time      `json:"completed_at,omitempty"`
}

type usageResponse struct {
	ID             uuid.UUID  `json:"id"`
	NodeAttemptID  *uuid.UUID `json:"node_attempt_id,omitempty"`
	Provider       string     `json:"provider"`
	Model          string     `json:"model"`
	PricingVersion *string    `json:"pricing_version,omitempty"`
	InputTokens    *int64     `json:"input_tokens,omitempty"`
	OutputTokens   *int64     `json:"output_tokens,omitempty"`
	CostMicrousd   *int64     `json:"cost_microusd,omitempty"`
	RecordedAt     time.Time  `json:"recorded_at"`
}

func mountRunRoutes(router chi.Router, repository RunRepository) {
	router.Post("/workflows/{workflowID}/runs", submitRunHandler(repository))
	router.Route("/runs", func(runs chi.Router) {
		runs.Get("/", listRunsHandler(repository))
		runs.Route("/{runID}", func(run chi.Router) {
			run.Get("/", getRunHandler(repository))
			run.Get("/nodes", listRunNodesHandler(repository))
			run.Get("/nodes/{nodeRunID}/attempts", listNodeAttemptsHandler(repository))
			run.Get("/events", replayEventsHandler(repository))
			run.Get("/usage", runUsageHandler(repository))
			run.Get("/stream", streamEventsHandler(repository))
			run.Post("/cancel", cancelRunHandler(repository))
		})
	})
}

func listNodeAttemptsHandler(repository RunRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ownerID, runID, ok := runRouteIDs(w, r)
		if !ok {
			return
		}
		nodeRunID, err := uuid.Parse(chi.URLParam(r, "nodeRunID"))
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "invalid_request", "node run id must be a UUID", nil)
			return
		}
		attempts, err := repository.ListNodeAttempts(r.Context(), ownerID, runID, nodeRunID)
		if err != nil {
			writeRunRepositoryError(w, r, err)
			return
		}
		items := make([]nodeAttemptResponse, 0, len(attempts))
		for _, attempt := range attempts {
			items = append(items, attemptFromDB(attempt))
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

func runUsageHandler(repository RunRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ownerID, runID, ok := runRouteIDs(w, r)
		if !ok {
			return
		}
		records, err := repository.ListRunUsage(r.Context(), ownerID, runID)
		if err != nil {
			writeRunRepositoryError(w, r, err)
			return
		}
		items := make([]usageResponse, 0, len(records))
		var inputTokens, outputTokens, knownCost int64
		costKnown := true
		for _, record := range records {
			items = append(items, usageResponse{
				ID: record.ID, NodeAttemptID: uuidFromPG(record.NodeAttemptID), Provider: record.Provider, Model: record.Model,
				PricingVersion: record.PricingVersion, InputTokens: record.InputTokens, OutputTokens: record.OutputTokens,
				CostMicrousd: record.CostMicrousd, RecordedAt: timeFromTimestamptz(record.RecordedAt),
			})
			if record.InputTokens != nil {
				inputTokens += *record.InputTokens
			}
			if record.OutputTokens != nil {
				outputTokens += *record.OutputTokens
			}
			if record.CostMicrousd == nil {
				costKnown = false
			} else {
				knownCost += *record.CostMicrousd
			}
		}
		var totalCost *int64
		if costKnown {
			totalCost = &knownCost
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"items":  items,
			"totals": map[string]any{"input_tokens": inputTokens, "output_tokens": outputTokens, "cost_microusd": totalCost},
		})
	}
}

func streamEventsHandler(repository RunRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ownerID, runID, ok := runRouteIDs(w, r)
		if !ok {
			return
		}
		lastSequence, err := parseLastEventID(r.Header.Get("Last-Event-ID"))
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeError(w, r, http.StatusInternalServerError, "streaming_unsupported", "streaming is unavailable", nil)
			return
		}
		if _, err := repository.GetRun(r.Context(), ownerID, runID); err != nil {
			writeRunRepositoryError(w, r, err)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache, no-transform")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		poll := time.NewTicker(750 * time.Millisecond)
		heartbeat := time.NewTicker(15 * time.Second)
		defer poll.Stop()
		defer heartbeat.Stop()
		for {
			events, err := repository.ReplayEvents(r.Context(), ownerID, runID, lastSequence, 500)
			if err != nil {
				return
			}
			for _, event := range events {
				payload, err := json.Marshal(eventFromDB(event))
				if err != nil {
					return
				}
				controller := http.NewResponseController(w)
				_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.Sequence, event.EventType, payload); err != nil {
					return
				}
				lastSequence = event.Sequence
			}
			if len(events) > 0 {
				flusher.Flush()
				continue
			}
			run, err := repository.GetRun(r.Context(), ownerID, runID)
			if err != nil || domain.RunStatus(run.Status).Terminal() {
				return
			}

			select {
			case <-r.Context().Done():
				return
			case <-heartbeat.C:
				controller := http.NewResponseController(w)
				_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
					return
				}
				flusher.Flush()
			case <-poll.C:
			}
		}
	}
}

func parseLastEventID(raw string) (int64, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	sequence, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || sequence < 0 {
		return 0, errors.New("Last-Event-ID must be a non-negative integer")
	}
	return sequence, nil
}

func submitRunHandler(repository RunRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ownerID, ok := ownerIDFromContext(r.Context())
		if !ok {
			writeError(w, r, http.StatusUnauthorized, "unauthorized", errInvalidOwner.Error(), nil)
			return
		}
		workflowID, err := uuid.Parse(chi.URLParam(r, "workflowID"))
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "invalid_request", "workflow id must be a UUID", nil)
			return
		}
		idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		if idempotencyKey == "" {
			writeError(w, r, http.StatusBadRequest, "idempotency_key_required", "Idempotency-Key header is required", nil)
			return
		}
		var request submitRunRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
			return
		}
		if len(request.Input) == 0 {
			request.Input = json.RawMessage(`{}`)
		}
		if request.CostBudgetMicrousd != nil && *request.CostBudgetMicrousd < 0 {
			writeError(w, r, http.StatusUnprocessableEntity, "validation_failed", "cost_budget_microusd must not be negative", nil)
			return
		}
		if request.Deadline != nil && request.Deadline.Before(time.Now().UTC()) {
			writeError(w, r, http.StatusUnprocessableEntity, "validation_failed", "deadline must be in the future", nil)
			return
		}
		submitted, err := repository.SubmitRun(r.Context(), store.SubmitRunParams{
			OwnerID: ownerID, WorkflowID: workflowID, IdempotencyKey: idempotencyKey,
			Input: request.Input, CostBudgetMicrousd: request.CostBudgetMicrousd, Deadline: request.Deadline,
		})
		if err != nil {
			writeRunRepositoryError(w, r, err)
			return
		}
		w.Header().Set("Location", "/api/v1/runs/"+submitted.Run.ID.String())
		if submitted.Existing {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		writeJSON(w, http.StatusAccepted, runFromDB(submitted.Run))
	}
}

func listRunsHandler(repository RunRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ownerID, ok := ownerIDFromContext(r.Context())
		if !ok {
			writeError(w, r, http.StatusUnauthorized, "unauthorized", errInvalidOwner.Error(), nil)
			return
		}
		pageSize, err := boundedPageSize(r.URL.Query().Get("page_size"))
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
			return
		}
		cursor, err := decodePageCursor(r.URL.Query().Get("cursor"))
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "invalid_request", "cursor is invalid", nil)
			return
		}
		var status *string
		if raw := strings.TrimSpace(r.URL.Query().Get("status")); raw != "" {
			candidate := domain.RunStatus(raw)
			if candidate != domain.RunPending && candidate != domain.RunRunning && !candidate.Terminal() {
				writeError(w, r, http.StatusBadRequest, "invalid_request", "invalid run status", nil)
				return
			}
			status = &raw
		}
		page, err := repository.ListRuns(r.Context(), ownerID, status, cursor, pageSize)
		if err != nil {
			writeRunRepositoryError(w, r, err)
			return
		}
		items := make([]runResponse, 0, len(page.Items))
		for _, run := range page.Items {
			items = append(items, runFromDB(run))
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": encodePageCursor(page.NextCursor)})
	}
}

func getRunHandler(repository RunRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ownerID, runID, ok := runRouteIDs(w, r)
		if !ok {
			return
		}
		run, err := repository.GetRun(r.Context(), ownerID, runID)
		if err != nil {
			writeRunRepositoryError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, runFromDB(run))
	}
}

func listRunNodesHandler(repository RunRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ownerID, runID, ok := runRouteIDs(w, r)
		if !ok {
			return
		}
		nodes, err := repository.ListRunNodes(r.Context(), ownerID, runID)
		if err != nil {
			writeRunRepositoryError(w, r, err)
			return
		}
		definition, err := repository.GetRunDefinition(r.Context(), ownerID, runID)
		if err != nil {
			writeRunRepositoryError(w, r, err)
			return
		}
		dependencies := make(map[string][]string, len(definition.Nodes))
		timeouts := make(map[string]int32, len(definition.Nodes))
		for _, node := range definition.Nodes {
			dependencies[node.ID] = node.DependsOn
			timeouts[node.ID] = node.TimeoutSeconds
		}
		items := make([]nodeRunResponse, 0, len(nodes))
		for _, node := range nodes {
			response := nodeFromDB(node)
			response.DependsOn = dependencies[node.NodeKey]
			response.TimeoutSeconds = timeouts[node.NodeKey]
			if response.TimeoutSeconds == 0 {
				response.TimeoutSeconds = 120
			}
			if response.DependsOn == nil {
				response.DependsOn = []string{}
			}
			items = append(items, response)
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

func replayEventsHandler(repository RunRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ownerID, runID, ok := runRouteIDs(w, r)
		if !ok {
			return
		}
		after, err := strconv.ParseInt(defaultString(r.URL.Query().Get("after"), "0"), 10, 64)
		if err != nil || after < 0 {
			writeError(w, r, http.StatusBadRequest, "invalid_request", "after must be a non-negative integer", nil)
			return
		}
		events, err := repository.ReplayEvents(r.Context(), ownerID, runID, after, 500)
		if err != nil {
			writeRunRepositoryError(w, r, err)
			return
		}
		items := make([]eventResponse, 0, len(events))
		for _, event := range events {
			items = append(items, eventFromDB(event))
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

func cancelRunHandler(repository RunRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ownerID, runID, ok := runRouteIDs(w, r)
		if !ok {
			return
		}
		run, err := repository.CancelRun(r.Context(), ownerID, runID)
		if err != nil {
			writeRunRepositoryError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, runFromDB(run))
	}
}

func runRouteIDs(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	ownerID, ok := ownerIDFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthorized", errInvalidOwner.Error(), nil)
		return uuid.Nil, uuid.Nil, false
	}
	runID, err := uuid.Parse(chi.URLParam(r, "runID"))
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "run id must be a UUID", nil)
		return uuid.Nil, uuid.Nil, false
	}
	return ownerID, runID, true
}

func writeRunRepositoryError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrIdempotencyConflict):
		writeError(w, r, http.StatusConflict, "idempotency_conflict", err.Error(), nil)
	case errors.Is(err, store.ErrIdempotencyInProgress):
		w.Header().Set("Retry-After", "1")
		writeError(w, r, http.StatusConflict, "idempotency_in_progress", err.Error(), nil)
	default:
		writeRepositoryError(w, r, err)
	}
}

func runFromDB(run db.WorkflowRun) runResponse {
	return runResponse{ID: run.ID, WorkflowVersionID: run.WorkflowVersionID, Status: run.Status, Input: run.Input, Output: run.Output,
		ErrorCode: run.ErrorCode, ErrorMessage: run.ErrorMessage, CostBudgetMicrousd: run.CostBudgetMicrousd,
		Deadline: optionalTime(run.DeadlineAt), CreatedAt: timeFromTimestamptz(run.CreatedAt),
		StartedAt: optionalTime(run.StartedAt), CompletedAt: optionalTime(run.CompletedAt)}
}

func nodeFromDB(node db.NodeRun) nodeRunResponse {
	return nodeRunResponse{ID: node.ID, NodeKey: node.NodeKey, NodeType: node.NodeType, Status: node.Status,
		CurrentAttemptNo: node.CurrentAttemptNo, Output: node.Output, ErrorCode: node.ErrorCode, ErrorMessage: node.ErrorMessage}
}

func eventFromDB(event db.ExecutionEvent) eventResponse {
	return eventResponse{ID: event.ID, Sequence: event.Sequence, EventType: event.EventType,
		NodeRunID: uuidFromPG(event.NodeRunID), AttemptID: uuidFromPG(event.AttemptID), Payload: event.Payload,
		OccurredAt: timeFromTimestamptz(event.OccurredAt)}
}

func attemptFromDB(attempt db.NodeAttempt) nodeAttemptResponse {
	return nodeAttemptResponse{
		ID: attempt.ID, NodeRunID: attempt.NodeRunID, AttemptNo: attempt.AttemptNo, Status: attempt.Status,
		WorkerID: attempt.WorkerID, FencingToken: attempt.FencingToken, LeaseExpiresAt: optionalTime(attempt.LeaseExpiresAt),
		LastHeartbeatAt: optionalTime(attempt.LastHeartbeatAt), Input: attempt.Input, Output: attempt.Output,
		ErrorCode: attempt.ErrorCode, ErrorMessage: attempt.ErrorMessage, Retryable: attempt.Retryable,
		QueuedAt: timeFromTimestamptz(attempt.QueuedAt), StartedAt: optionalTime(attempt.StartedAt), CompletedAt: optionalTime(attempt.CompletedAt),
	}
}

func optionalTime(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	timestamp := value.Time.UTC()
	return &timestamp
}

func uuidFromPG(value pgtype.UUID) *uuid.UUID {
	if !value.Valid {
		return nil
	}
	id := uuid.UUID(value.Bytes)
	return &id
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
