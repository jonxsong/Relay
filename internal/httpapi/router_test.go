package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jonxsong/Relay/internal/domain"
	"github.com/jonxsong/Relay/internal/store"
	"github.com/jonxsong/Relay/internal/store/db"
)

type fakeReadiness struct{ err error }

func (f fakeReadiness) Ready(context.Context) error { return f.err }

type fakeWorkflows struct {
	create func(context.Context, uuid.UUID, string, string, domain.WorkflowDefinition) (store.CreatedWorkflow, error)
	list   func(context.Context, uuid.UUID, *store.PageCursor, int32) (store.WorkflowPage, error)
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

func (f fakeWorkflows) ListWorkflows(ctx context.Context, ownerID uuid.UUID, cursor *store.PageCursor, pageSize int32) (store.WorkflowPage, error) {
	if f.list != nil {
		return f.list(ctx, ownerID, cursor, pageSize)
	}
	return store.WorkflowPage{Items: []db.Workflow{}}, nil
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

func TestWorkflowPaginationCursorRoundTrip(t *testing.T) {
	t.Parallel()
	ownerID := uuid.New()
	workflowID := uuid.New()
	createdAt := time.Date(2026, 11, 14, 23, 0, 0, 0, time.UTC)
	repository := fakeWorkflows{list: func(_ context.Context, owner uuid.UUID, cursor *store.PageCursor, pageSize int32) (store.WorkflowPage, error) {
		if owner != ownerID || cursor != nil || pageSize != 1 {
			t.Fatalf("unexpected list inputs: owner=%s cursor=%+v pageSize=%d", owner, cursor, pageSize)
		}
		return store.WorkflowPage{
			Items:      []db.Workflow{{ID: workflowID, OwnerID: ownerID, Name: "paged", CreatedAt: pgtype.Timestamptz{Time: createdAt, Valid: true}}},
			NextCursor: &store.PageCursor{CreatedAt: createdAt, ID: workflowID},
		}, nil
	}}
	router := NewRouter(Dependencies{Readiness: fakeReadiness{}, Workflows: repository, DevelopmentUser: ownerID, MaxRequestBytes: 4096})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/workflows/?page_size=1", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	var body struct {
		NextCursor *string `json:"next_cursor"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.NextCursor == nil {
		t.Fatalf("decode response cursor: %v, body=%s", err, response.Body.String())
	}
	decoded, err := decodePageCursor(*body.NextCursor)
	if err != nil || decoded.ID != workflowID || !decoded.CreatedAt.Equal(createdAt) {
		t.Fatalf("decoded cursor = %+v, err=%v", decoded, err)
	}

	invalid := httptest.NewRequest(http.MethodGet, "/api/v1/workflows/?cursor=not-valid", nil)
	invalidResponse := httptest.NewRecorder()
	router.ServeHTTP(invalidResponse, invalid)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid cursor status = %d, want 400", invalidResponse.Code)
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

func TestProductionRequiresBearerAuthentication(t *testing.T) {
	t.Parallel()
	const apiKey = "relay_test_0123456789abcdef0123456789abcdef"
	router := NewRouter(Dependencies{Readiness: fakeReadiness{}, Workflows: fakeWorkflows{}, DevelopmentUser: uuid.New(), MaxRequestBytes: 1024, Environment: "production", APIKey: apiKey})

	unauthorized := httptest.NewRequest(http.MethodGet, "/api/v1/workflows/", nil)
	unauthorizedResponse := httptest.NewRecorder()
	router.ServeHTTP(unauthorizedResponse, unauthorized)
	if unauthorizedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want %d", unauthorizedResponse.Code, http.StatusUnauthorized)
	}

	authorized := httptest.NewRequest(http.MethodGet, "/api/v1/workflows/", nil)
	authorized.Header.Set("Authorization", "Bearer "+apiKey)
	authorizedResponse := httptest.NewRecorder()
	router.ServeHTTP(authorizedResponse, authorized)
	if authorizedResponse.Code != http.StatusOK {
		t.Fatalf("authenticated status = %d, want %d: %s", authorizedResponse.Code, http.StatusOK, authorizedResponse.Body.String())
	}
}

func TestRateLimitReturnsRetryMetadata(t *testing.T) {
	t.Parallel()
	router := NewRouter(Dependencies{Readiness: fakeReadiness{}, Workflows: fakeWorkflows{}, DevelopmentUser: uuid.New(), MaxRequestBytes: 1024, RateLimitPerMinute: 1})

	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/api/v1/health/live", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d", first.Code)
	}
	second := httptest.NewRecorder()
	router.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/api/v1/health/live", nil))
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want %d", second.Code, http.StatusTooManyRequests)
	}
	if second.Header().Get("Retry-After") == "" || second.Header().Get("X-RateLimit-Reset") == "" {
		t.Fatal("rate limit response is missing retry metadata")
	}
}

func TestMetricsEndpoint(t *testing.T) {
	t.Parallel()
	router := NewRouter(Dependencies{Readiness: fakeReadiness{}, Workflows: fakeWorkflows{}, DevelopmentUser: uuid.New(), MaxRequestBytes: 1024})
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/health/live", nil))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	if !bytes.Contains(response.Body.Bytes(), []byte("relay_http_requests_total")) {
		t.Fatal("metrics response does not contain Relay HTTP metrics")
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
