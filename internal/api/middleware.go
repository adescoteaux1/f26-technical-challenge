package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/adescoteaux1/generate-control-tower/internal/models"
	"github.com/adescoteaux1/generate-control-tower/internal/store"
)

type contextKey int

const userContextKey contextKey = iota

// publicOperations lists the operation IDs that don't require a bearer token.
var publicOperations = map[string]bool{
	"register":              true,
	"login":                 true,
	"apply":                 true,
	"frontend-hello":        true,
	"portal-network-status": true,
	"list-bookings":         true,
}

// authMiddleware extracts a bearer token from the Authorization header,
// looks up the corresponding user, and attaches it to the request context
// for every operation except those in publicOperations.
func authMiddleware(api huma.API, st store.Store) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		if publicOperations[ctx.Operation().OperationID] {
			next(ctx)
			return
		}

		token := bearerToken(ctx.Header("Authorization"))
		if token == "" {
			huma.WriteErr(api, ctx, http.StatusUnauthorized, "missing Authorization: Bearer <token> header")
			return
		}

		user, err := st.GetUserByToken(ctx.Context(), token)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				huma.WriteErr(api, ctx, http.StatusUnauthorized, "invalid or expired token")
				return
			}
			huma.WriteErr(api, ctx, http.StatusInternalServerError, "internal error")
			return
		}

		next(huma.WithValue(ctx, userContextKey, user))
	}
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, prefix))
}

func userFromContext(ctx context.Context) *models.User {
	user, _ := ctx.Value(userContextKey).(*models.User)
	return user
}
