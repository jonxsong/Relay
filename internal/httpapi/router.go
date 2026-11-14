package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
)

type ReadinessChecker interface {
	Ready(context.Context) error
}

type Dependencies struct {
	Readiness       ReadinessChecker
	Workflows       WorkflowRepository
	Runs            RunRepository
	DevelopmentUser uuid.UUID
	MaxRequestBytes int64
}

func NewRouter(deps Dependencies) http.Handler {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(securityHeaders)
	router.Use(requestLogger)
	router.Use(recoverer)
	router.Use(middleware.Timeout(30 * time.Second))
	router.Use(maxBodyBytes(deps.MaxRequestBytes))

	router.Route("/api/v1", func(api chi.Router) {
		api.Get("/health/live", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		})
		api.Get("/health/ready", readinessHandler(deps.Readiness))

		api.Group(func(authenticated chi.Router) {
			authenticated.Use(developmentAuth(deps.DevelopmentUser))
			mountWorkflowRoutes(authenticated, deps.Workflows)
			mountRunRoutes(authenticated, deps.Runs)
		})
	})
	router.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusNotFound, "not_found", "resource not found", nil)
	})
	router.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
	})
	return router
}

func readinessHandler(checker ReadinessChecker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if checker == nil || checker.Ready(ctx) != nil {
			writeError(w, r, http.StatusServiceUnavailable, "not_ready", "service is not ready", nil)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	}
}

func maxBodyBytes(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil && limit > 0 {
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}
			next.ServeHTTP(w, r)
		})
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		wrapper := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(wrapper, r)
		slog.InfoContext(r.Context(), "request",
			"request_id", middleware.GetReqID(r.Context()),
			"method", r.Method,
			"path", r.URL.Path,
			"status", wrapper.Status(),
			"duration_ms", time.Since(started).Milliseconds(),
		)
	})
}

func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.ErrorContext(r.Context(), "panic recovered", "panic", recovered, "stack", string(debug.Stack()))
				writeError(w, r, http.StatusInternalServerError, "internal_error", "an internal error occurred", nil)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

var errInvalidOwner = errors.New("authenticated owner is unavailable")
