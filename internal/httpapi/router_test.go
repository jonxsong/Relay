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

type fakeReadiness struct{ err error }

func (f fakeReadiness) Ready(context.Context) error { return f.err }

type fakeWorkflows struct {
	create func(context.Context, uuid.UUID, string, string, domain.WorkflowDefinition) (store.CreatedWorkflow, error)
}

func (f fakeWorkflows) CreateWorkflow(ctx context.Context, owner uuid.UUID, name, description string, definition domain.WorkflowDefinition) (store.CreatedWorkflow, error) {
	if f.create == nil {
		return store.CreatedWorkflow{}, errors.New("unexpected CreateWorkflow call")
	}
	return f.create(ctx, owner, name, description, definition)
}
func (fakeWorkflows) CreateWorkflowVersion(context.Context, uuid.UUID, uuid.UUID, domain.WorkflowDefinition) (db.WorkflowVersion, error) {
	return db.WorkflowVersion{}, errors.New("not implemented in fake")
}
func (fakeWorkflows) GetWorkflow(context.Context, uuid.UUID, uuid.UUID) (db.Workflow, error) {
	return db.Workflow{}, store.ErrNotFound
}
func (fakeWorkflows) GetLatestWorkflowVersion(context.Context, uuid.UUID, uuid.UUID) (db.WorkflowVersion, error) {
	return db.WorkflowVersion{}, store.ErrNotFound
}
func (fakeWorkflows) ListWorkflows(context.Context, uuid.UUID, int32) ([]db.Workflow, error) {
	return []db.Workflow{}, nil
}

func TestHealthEndpoints(t *testing.T) {
	t.Parallel()
	ownerID := uuid.New()
	router := NewRouter(Dependencies{Readiness: fakeReadiness{}, Workflows: fakeWorkflows{}, DevelopmentUser: ownerID, MaxRequestBytes: 1024})
	for _, path := range []string{"/api/v1/health/live", "/api/v1/health/ready"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Errorf("GET %s status = %d, want %d", path, response.Code, http.StatusOK)
		}
	}
}

func TestReadinessDoesNotLeakDependencyError(t *testing.T) {
	t.Parallel()
	router := NewRouter(Dependencies{Readiness: fakeReadiness{err: errors.New("password secret")}, Workflows: fakeWorkflows{}, DevelopmentUser: uuid.New(), MaxRequestBytes: 1024})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/health/ready", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	if bytes.Contains(response.Body.Bytes(), []byte("password secret")) {
		t.Fatal("readiness response leaked dependency error")
	}
}

func TestCreateWorkflowRejectsUnknownFields(t *testing.T) {
	t.Parallel()
	router := NewRouter(Dependencies{Readiness: fakeReadiness{}, Workflows: fakeWorkflows{}, DevelopmentUser: uuid.New(), MaxRequestBytes: 4096})
	body := bytes.NewBufferString(`{"name":"demo","definition":{"name":"demo","nodes":[]},"surprise":true}`)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/", body)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusBadRequest, response.Body.String())
	}
}

func TestCreateWorkflowUsesAuthenticatedOwner(t *testing.T) {
	t.Parallel()
	ownerID := uuid.New()
	workflowID := uuid.New()
	versionID := uuid.New()
	repository := fakeWorkflows{create: func(_ context.Context, gotOwner uuid.UUID, name, _ string, definition domain.WorkflowDefinition) (store.CreatedWorkflow, error) {
		if gotOwner != ownerID {
			t.Fatalf("owner = %s, want %s", gotOwner, ownerID)
		}
		if name != "demo" || len(definition.Nodes) != 1 {
			t.Fatalf("unexpected workflow payload: name=%q nodes=%d", name, len(definition.Nodes))
		}
		return store.CreatedWorkflow{
			Workflow: db.Workflow{ID: workflowID, OwnerID: ownerID, Name: "demo"},
			Version:  db.WorkflowVersion{ID: versionID, WorkflowID: workflowID, Version: 1},
		}, nil
	}}
	router := NewRouter(Dependencies{Readiness: fakeReadiness{}, Workflows: repository, DevelopmentUser: ownerID, MaxRequestBytes: 4096})
	body := bytes.NewBufferString(`{"name":"demo","definition":{"nodes":[{"id":"start","type":"transform"}]}}`)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/", body)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusCreated, response.Body.String())
	}
	if got := response.Header().Get("Location"); got != "/api/v1/workflows/"+workflowID.String() {
		t.Fatalf("Location = %q", got)
	}
}
