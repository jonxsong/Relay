package httpapi

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

type ownerContextKey struct{}

func developmentAuth(ownerID uuid.UUID) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), ownerContextKey{}, ownerID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func ownerIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	ownerID, ok := ctx.Value(ownerContextKey{}).(uuid.UUID)
	return ownerID, ok && ownerID != uuid.Nil
}
