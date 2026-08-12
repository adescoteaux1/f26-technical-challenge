package api

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// NewRouter wires every Control Tower endpoint through Huma, which generates an
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

	r.Get("/", landingPageHandler)
	r.Get("/style.css", stylesheetHandler)
	r.Get("/no-copy.js", noCopyScriptHandler)
	r.Get("/challenge", challengePageHandler)
	r.Get("/frontend-challenge", frontendChallengePageHandler)
	r.Get("/apply", applyPageHandler)

	config := huma.DefaultConfig("Nexus Transit Authority — Control Tower", "1.0.0")
	config.Info.Description = "Scheduling challenge Control Tower: register, start an expedition, " +
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

	huma.Register(api, huma.Operation{
		OperationID: "apply",
		Method:      http.MethodPost,
		Path:        "/apply",
		Summary:     "Get a challenge repo",
		Description: "Give a GitHub username to get a private repo under the org with push " +
			"access already granted, instead of creating and sharing your own repo. Safe to call " +
			"again with the same username — it reuses the existing repo rather than erroring.",
		Tags:          []string{"Apply"},
		DefaultStatus: http.StatusCreated,
	}, s.applyHandler)

	huma.Register(api, huma.Operation{
		OperationID: "chaos-probe",
		Method:      http.MethodGet,
		Path:        "/chaos/probe",
		Summary:     "Simulate a flaky network response, for testing your client's own resilience",
		Description: "Deterministic and stateless: same query params always produce the same " +
			"outcome, so you can write a real test against it instead of hoping your retry " +
			"logic works. Not connected to any expedition or score.",
		Tags:     []string{"Testing"},
		Security: []map[string][]string{{"bearer": {}}},
	}, s.chaosProbeHandler)

	huma.Register(api, huma.Operation{
		OperationID: "portal-network-status",
		Method:      http.MethodGet,
		Path:        "/frontend/portals",
		Summary:     "Portal Network Status for the operations console",
		Description: "Returns all six portals with a randomized load percentage per request; " +
			"status is derived from the percentage, not sent independently.",
		Tags: []string{"Frontend"},
	}, s.portalStatusHandler)

	huma.Register(api, huma.Operation{
		OperationID: "list-bookings",
		Method:      http.MethodGet,
		Path:        "/frontend/bookings",
		Summary:     "Paginated transit bookings for the operations console",
		Description: "Cursor-paginated for infinite scroll: feed nextCursor back as ?cursor= to " +
			"append the next batch. There are far more bookings on file than one request can " +
			"return — limit is capped at 10, so the console has to page through them.",
		Tags: []string{"Frontend"},
	}, s.bookingsHandler)

	return r
}
