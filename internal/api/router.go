package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// NewRouter wires the three Oracle endpoints described in the challenge spec.
func NewRouter(s *Server) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	r.Post("/register", s.register)
	r.Post("/login", s.login)

	r.Group(func(r chi.Router) {
		r.Use(requireAuth(s.Store))

		r.Post("/evaluation", s.createEvaluation)
		r.Get("/evaluation/{id}", s.getEvaluation)
		r.Post("/simulation/{id}/schedule", s.submitSchedule)
		r.Get("/me/evaluations", s.history)
	})

	return r
}
