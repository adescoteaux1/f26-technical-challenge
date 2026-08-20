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

	// A mid-cycle surge_arrivals state response is ~240KB of JSON that gzips
	// to ~12KB, once per tick for 300+ ticks. The explicit type list replaces
	// chi's default allowlist rather than extending it, so the OpenAPI and
	// huma error types have to be named or they'd be missed; image/png is
	// omitted on purpose, since gzipping the design exports grows them.
	r.Use(middleware.Compress(5, "text/*", "application/json",
		"application/openapi+json", "application/openapi+yaml", "application/problem+json"))

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	r.Get("/", landingPageHandler)
	r.Handle("/site/assets/*", assetsHandler())
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
		OperationID: "admin-lookup",
		Method:      http.MethodGet,
		Path:        "/admin/lookup",
		Summary:     "Look up a candidate's submission history",
		Description: "Search by user ID and/or email (id takes precedence if both are given). " +
			"Gated by the X-Admin-Token header, not a user bearer token — there's no user account " +
			"this belongs to. Returns 503 if ADMIN_TOKEN isn't configured on this server.",
		Tags: []string{"Admin"},
	}, s.adminLookupHandler)

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
		Description: "Returns all six portals with a randomized load; status is derived from it, " +
			"not sent independently. The network state is stable for the whole clock hour, so " +
			"polling more often than that returns the same values.",
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

	huma.Register(api, huma.Operation{
		OperationID: "list-transit-slots",
		Method:      http.MethodGet,
		Path:        "/frontend/slots",
		Summary:     "Bookable transit slots",
		Description: "The inventory a booking flow is built from. Stable across restarts, so a " +
			"slot ID stays valid; how you present, filter or sequence them is up to you.",
		Tags: []string{"Frontend"},
	}, s.transitSlotsHandler)

	huma.Register(api, huma.Operation{
		OperationID: "submit-booking",
		Method:      http.MethodPost,
		Path:        "/frontend/slots/{slotId}/book",
		Summary:     "Submit a booking for a slot",
		Description: "Roughly 30% of submissions fail at random. Every response carries the same " +
			"body, so reading status is enough to tell what happened, and retryable says whether " +
			"resubmitting unchanged could work. Outcomes that are real answers (confirmed, " +
			"slot_taken, insufficient_seats) return 200; a corridor failure returns 503 because " +
			"the booking never completed. Use GET /chaos/probe when you need a reproducible " +
			"failure for a test.",
		Tags: []string{"Frontend"},
	}, s.submitBookingHandler)

	return r
}
