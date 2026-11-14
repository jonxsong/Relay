package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jonxsong/Relay/internal/telemetry"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/trace"
)

type ReadinessChecker interface {
	Ready(context.Context) error
}

type Dependencies struct {
	Readiness          ReadinessChecker
	Workflows          WorkflowRepository
	Runs               RunRepository
	DevelopmentUser    uuid.UUID
	MaxRequestBytes    int64
	Environment        string
	APIKey             string
	RateLimitPerMinute int
}

func NewRouter(deps Dependencies) http.Handler {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(securityHeaders)
	router.Use(requestLogger)
	router.Use(recoverer)
	router.Use(requestTimeout(30 * time.Second))
	router.Use(maxBodyBytes(deps.MaxRequestBytes))
	router.Use(telemetry.HTTPMetrics)
	if deps.RateLimitPerMinute > 0 {
		router.Use(rateLimit(deps.RateLimitPerMinute, time.Minute))
	}
	router.Handle("/metrics", promhttp.Handler())

	router.Route("/api/v1", func(api chi.Router) {
		api.Get("/health/live", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		})
		api.Get("/health/ready", readinessHandler(deps.Readiness))

		api.Group(func(authenticated chi.Router) {
			authenticated.Use(authentication(deps.Environment, deps.APIKey, deps.DevelopmentUser))
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
	return otelhttp.NewHandler(router, "relay.api")
}

type requestWindow struct {
	count   int
	resetAt time.Time
}

func rateLimit(limit int, window time.Duration) func(http.Handler) http.Handler {
	var mu sync.Mutex
	clients := make(map[string]requestWindow)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			now := time.Now()
			client := r.RemoteAddr
			if authorization := r.Header.Get("Authorization"); authorization != "" {
				digest := sha256.Sum256([]byte(authorization))
				client = hex.EncodeToString(digest[:])
			}
			if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
				if r.Header.Get("Authorization") == "" {
					client = host
				}
			}
			mu.Lock()
			entry := clients[client]
			if entry.resetAt.IsZero() || !now.Before(entry.resetAt) {
				entry = requestWindow{resetAt: now.Add(window)}
			}
			entry.count++
			clients[client] = entry
			remaining := max(0, limit-entry.count)
			mu.Unlock()

			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(entry.resetAt.Unix(), 10))
			if entry.count > limit {
				w.Header().Set("Retry-After", strconv.Itoa(max(1, int(time.Until(entry.resetAt).Seconds()))))
				writeError(w, r, http.StatusTooManyRequests, "rate_limited", "request rate limit exceeded", nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func authentication(environment, apiKey string, ownerID uuid.UUID) func(http.Handler) http.Handler {
	if environment == "" || environment == "development" {
		return developmentAuth(ownerID)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			const prefix = "Bearer "
			header := r.Header.Get("Authorization")
			provided := ""
			if strings.HasPrefix(header, prefix) {
				provided = strings.TrimSpace(strings.TrimPrefix(header, prefix))
			}
			if apiKey == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(apiKey)) != 1 {
				w.Header().Set("WWW-Authenticate", `Bearer realm="relay"`)
				writeError(w, r, http.StatusUnauthorized, "unauthorized", "valid bearer authentication is required", nil)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ownerContextKey{}, ownerID)))
		})
	}
}

func requestTimeout(timeout time.Duration) func(http.Handler) http.Handler {
	regularRequests := middleware.Timeout(timeout)
	return func(next http.Handler) http.Handler {
		timed := regularRequests(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/stream") {
				next.ServeHTTP(w, r)
				return
			}
			timed.ServeHTTP(w, r)
		})
	}
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
			"trace_id", trace.SpanContextFromContext(r.Context()).TraceID().String(),
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
