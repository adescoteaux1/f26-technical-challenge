package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/adescoteaux1/generate-oracle/internal/models"
	"github.com/adescoteaux1/generate-oracle/internal/store"
)

type contextKey int

const userContextKey contextKey = iota

// requireAuth extracts a bearer token from the Authorization header, looks
// up the corresponding user, and attaches it to the request context. Every
// endpoint except /register, /login, and /healthz is wrapped in this.
func requireAuth(st store.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := bearerToken(r)
			if token == "" {
				writeError(w, http.StatusUnauthorized, "missing Authorization: Bearer <token> header")
				return
			}

			user, err := st.GetUserByToken(r.Context(), token)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					writeError(w, http.StatusUnauthorized, "invalid or expired token")
					return
				}
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}

			ctx := context.WithValue(r.Context(), userContextKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, prefix))
}

func userFromContext(r *http.Request) *models.User {
	user, _ := r.Context().Value(userContextKey).(*models.User)
	return user
}
