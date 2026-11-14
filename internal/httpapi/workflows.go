package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
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

type WorkflowRepository interface {
	CreateWorkflow(context.Context, uuid.UUID, string, string, domain.WorkflowDefinition) (store.CreatedWorkflow, error)
	CreateWorkflowVersion(context.Context, uuid.UUID, uuid.UUID, domain.WorkflowDefinition) (db.WorkflowVersion, error)
	GetWorkflow(context.Context, uuid.UUID, uuid.UUID) (db.Workflow, error)
	ListWorkflows(context.Context, uuid.UUID, int32) ([]db.Workflow, error)
}

type createWorkflowRequest struct {
	Name        string                    `json:"name"`
	Description string                    `json:"description,omitempty"`
	Definition  domain.WorkflowDefinition `json:"definition"`
}

type createVersionRequest struct {
	Definition domain.WorkflowDefinition `json:"definition"`
}

type workflowResponse struct {
	ID          uuid.UUID        `json:"id"`
	Name        string           `json:"name"`
	Description string           `json:"description"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
	Version     *versionResponse `json:"version,omitempty"`
}

type versionResponse struct {
	ID         uuid.UUID `json:"id"`
	WorkflowID uuid.UUID `json:"workflow_id"`
	Version    int32     `json:"version"`
	CreatedAt  time.Time `json:"created_at"`
}

func mountWorkflowRoutes(router chi.Router, repository WorkflowRepository) {
	router.Route("/workflows", func(workflows chi.Router) {
		workflows.Post("/", createWorkflowHandler(repository))
		workflows.Get("/", listWorkflowsHandler(repository))
		workflows.Route("/{workflowID}", func(workflow chi.Router) {
			workflow.Get("/", getWorkflowHandler(repository))
			workflow.Post("/versions", createWorkflowVersionHandler(repository))
		})
	})
}

func createWorkflowHandler(repository WorkflowRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ownerID, ok := ownerIDFromContext(r.Context())
		if !ok {
			writeError(w, r, http.StatusUnauthorized, "unauthorized", errInvalidOwner.Error(), nil)
			return
		}
		var request createWorkflowRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
			return
		}
		created, err := repository.CreateWorkflow(r.Context(), ownerID, request.Name, request.Description, request.Definition)
		if err != nil {
			writeRepositoryError(w, r, err)
			return
		}
		response := workflowFromDB(created.Workflow)
		version := versionFromDB(created.Version)
		response.Version = &version
		w.Header().Set("Location", "/api/v1/workflows/"+created.Workflow.ID.String())
		writeJSON(w, http.StatusCreated, response)
	}
}

func listWorkflowsHandler(repository WorkflowRepository) http.HandlerFunc {
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
		workflows, err := repository.ListWorkflows(r.Context(), ownerID, pageSize)
		if err != nil {
			writeRepositoryError(w, r, err)
			return
		}
		items := make([]workflowResponse, 0, len(workflows))
		for _, workflow := range workflows {
			items = append(items, workflowFromDB(workflow))
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": nil})
	}
}

func getWorkflowHandler(repository WorkflowRepository) http.HandlerFunc {
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
		workflow, err := repository.GetWorkflow(r.Context(), ownerID, workflowID)
		if err != nil {
			writeRepositoryError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, workflowFromDB(workflow))
	}
}

func createWorkflowVersionHandler(repository WorkflowRepository) http.HandlerFunc {
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
		var request createVersionRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
			return
		}
		version, err := repository.CreateWorkflowVersion(r.Context(), ownerID, workflowID, request.Definition)
		if err != nil {
			writeRepositoryError(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, versionFromDB(version))
	}
}

func decodeJSON(r *http.Request, destination any) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return errors.New("Content-Type must be application/json")
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			return fmt.Errorf("request body exceeds %d bytes", maxBytesError.Limit)
		}
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain a single JSON object")
	}
	return nil
}

func boundedPageSize(raw string) (int32, error) {
	if strings.TrimSpace(raw) == "" {
		return 50, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > 100 {
		return 0, errors.New("page_size must be an integer between 1 and 100")
	}
	return int32(value), nil
}

func writeRepositoryError(w http.ResponseWriter, r *http.Request, err error) {
	var validationErrors domain.ValidationErrors
	switch {
	case errors.As(err, &validationErrors):
		writeError(w, r, http.StatusUnprocessableEntity, "validation_failed", "workflow definition is invalid", validationErrors)
	case errors.Is(err, store.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", "resource not found", nil)
	case errors.Is(err, store.ErrConflict):
		writeError(w, r, http.StatusConflict, "conflict", "resource conflicts with existing state", nil)
	default:
		writeError(w, r, http.StatusInternalServerError, "internal_error", "an internal error occurred", nil)
	}
}

func workflowFromDB(workflow db.Workflow) workflowResponse {
	return workflowResponse{
		ID: workflow.ID, Name: workflow.Name, Description: workflow.Description,
		CreatedAt: timeFromTimestamptz(workflow.CreatedAt), UpdatedAt: timeFromTimestamptz(workflow.UpdatedAt),
	}
}

func versionFromDB(version db.WorkflowVersion) versionResponse {
	return versionResponse{
		ID: version.ID, WorkflowID: version.WorkflowID, Version: version.Version, CreatedAt: timeFromTimestamptz(version.CreatedAt),
	}
}

func timeFromTimestamptz(value pgtype.Timestamptz) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time.UTC()
}
