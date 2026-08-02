package api

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// NewRouter wires every Oracle endpoint through Huma, which generates an
// OpenAPI 3.1 spec from the Input/Output types in dto.go and serves it as
// interactive Scalar docs at /docs (raw spec at /openapi.json|yaml).
func NewRouter(s *Server) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	config := huma.DefaultConfig("Nexus Transit Authority — Oracle", "1.0.0")
	config.Info.Description = "Scheduling challenge Oracle: register, start an expedition, " +
		"and submit gate/voyage assignments each cycle tick."
	config.DocsPath = "/docs"
	config.DocsRenderer = huma.DocsRendererScalar
	config.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"bearer": {
			Type:        "http",
			Scheme:      "bearer",
			Description: "Token returned by POST /register or POST /login.",
		},
	}

	api := humachi.New(r, config)
	api.UseMiddleware(authMiddleware(api, s.Store))

	huma.Register(api, huma.Operation{
		OperationID:   "register",
		Method:        http.MethodPost,
		Path:          "/register",
		Summary:       "Register a new account",
		Tags:          []string{"Auth"},
		DefaultStatus: http.StatusCreated,
	}, s.registerHandler)

	huma.Register(api, huma.Operation{
		OperationID: "login",
		Method:      http.MethodPost,
		Path:        "/login",
		Summary:     "Log in and rotate your bearer token",
		Tags:        []string{"Auth"},
	}, s.loginHandler)

	huma.Register(api, huma.Operation{
		OperationID: "list-expeditions",
		Method:      http.MethodGet,
		Path:        "/me/expeditions",
		Summary:     "List your past expeditions",
		Tags:        []string{"Expeditions"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, s.historyHandler)

	huma.Register(api, huma.Operation{
		OperationID:   "create-expedition",
		Method:        http.MethodPost,
		Path:          "/expedition",
		Summary:       "Start a new expedition",
		Tags:          []string{"Expeditions"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusCreated,
	}, s.createExpeditionHandler)

	huma.Register(api, huma.Operation{
		OperationID: "get-expedition",
		Method:      http.MethodGet,
		Path:        "/expedition/{id}",
		Summary:     "Get the current state of an expedition",
		Tags:        []string{"Expeditions"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, s.getExpeditionHandler)

	huma.Register(api, huma.Operation{
		OperationID: "submit-cycle",
		Method:      http.MethodPost,
		Path:        "/cycle/{id}/schedule",
		Summary:     "Submit gate/voyage assignments for the current tick",
		Tags:        []string{"Expeditions"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, s.submitCycleHandler)

	return r
}
