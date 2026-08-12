package api

import (
	"bytes"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"runtime"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"

	"github.com/adescoteaux1/generate-control-tower/site"
)

// markdownRenderer enables GFM (tables, strikethrough, autolinks, ...) —
// plain CommonMark doesn't support tables, and CHALLENGE.md relies on them
// heavily (the evaluation rubric, workload profile list, ...).
var markdownRenderer = goldmark.New(goldmark.WithExtensions(extension.GFM))

// repoRootPath resolves a path relative to the repo root regardless of the
// process's current working directory (computed from this source file's
// own location at build time, not assumed relative to cwd) — so it works
// the same whether the server is launched from the repo root or a test
// runs it from internal/api/.
func repoRootPath(parts ...string) string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(append([]string{filepath.Dir(thisFile), "..", ".."}, parts...)...)
}

type resourceLink struct{ Label, URL string }

// challengeResources are the "helpful resources" links shown at the bottom
// of the rendered backend challenge spec page. This is deliberately not
// part of CHALLENGE.md itself — that file is the portable spec text (and
// may be distributed on its own); these links are site-only chrome around it.
var challengeResources = []resourceLink{
	{"MDN: HTTP overview", "https://developer.mozilla.org/en-US/docs/Web/HTTP/Overview"},
	{"MDN: HTTP response status codes", "https://developer.mozilla.org/en-US/docs/Web/HTTP/Status"},
	{"OpenAPI Specification", "https://spec.openapis.org/oas/latest.html"},
	{"Scalar — this Control Tower's live API reference", "https://scalar.com/"},
	{"AWS: Timeouts, retries, and backoff with jitter", "https://aws.amazon.com/builders-library/timeouts-retries-and-backoff-with-jitter"},
	{"Stripe: Designing robust and predictable APIs with idempotency", "https://stripe.com/blog/idempotency"},
	{"MDN: Working with JSON", "https://developer.mozilla.org/en-US/docs/Learn/JavaScript/Objects/JSON"},
	{"JSON Schema", "https://json-schema.org/"},
	{"Martin Fowler: Test Pyramid", "https://martinfowler.com/bliki/TestPyramid.html"},
	{"Martin Fowler: Test Double", "https://martinfowler.com/bliki/TestDouble.html"},
}

// frontendChallengeResources are shown under the operations console spec.
// Framework-neutral on purpose, and limited to things an applicant wouldn't
// trip over on their own: a build tool or component library they'd pick in
// the first five minutes doesn't belong here.
var frontendChallengeResources = []resourceLink{
	{"Tailwind CSS", "https://tailwindcss.com/docs"},
	{"MDN: Intersection Observer — triggering the next page on scroll", "https://developer.mozilla.org/en-US/docs/Web/API/Intersection_Observer_API"},
	{"MDN: AbortController — cancelling in-flight requests", "https://developer.mozilla.org/en-US/docs/Web/API/AbortController"},
	{"TanStack Query — caching and revalidation, with adapters for most frameworks", "https://tanstack.com/query/latest"},
	{"Nielsen Norman Group: error message guidelines", "https://www.nngroup.com/articles/error-message-guidelines/"},
	{"web.dev: Learn Accessibility", "https://web.dev/learn/accessibility"},
}

var markdownPageTemplate = template.Must(template.New("markdown-page").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<link rel="stylesheet" href="/style.css">
<script src="/no-copy.js" defer></script>
</head>
<body>
<header class="site-header">
  <a href="/" class="brand">← Nexus Transit Authority</a>
  <a href="/docs" class="docs-link">API Reference (Scalar) →</a>
</header>
<article class="markdown-body">
{{.Content}}
</article>
{{if .Resources}}
<section class="resources">
  <h2>Resources</h2>
  <p>Things that might help while you build, unrelated to this specific challenge:</p>
  <ul>
    {{range .Resources}}<li><a href="{{.URL}}" target="_blank" rel="noopener">{{.Label}}</a></li>
    {{end}}
  </ul>
</section>
{{end}}
</body>
</html>
`))

// markdownPage returns a handler that renders the markdown file at mdPath
// from disk into HTML on every request (not embedded, so edits show up on
// reload with no rebuild), wraps it in the shared page shell, and appends
// the given resources list (may be nil to omit that section entirely).
func markdownPage(mdPath, title string, resources []resourceLink) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, err := os.ReadFile(mdPath)
		if err != nil {
			http.Error(w, "page content not found", http.StatusInternalServerError)
			return
		}

		var rendered bytes.Buffer
		if err := markdownRenderer.Convert(raw, &rendered); err != nil {
			http.Error(w, "failed to render page content", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = markdownPageTemplate.Execute(w, struct {
			Title     string
			Content   template.HTML
			Resources []resourceLink
		}{
			Title:     title,
			Content:   template.HTML(rendered.String()), //nolint:gosec // trusted repo file, not user input
			Resources: resources,
		})
	}
}

// assetsHandler serves the embedded design exports. The route is mounted at
// the assets' own repo path (/site/assets/...) so that a relative image link
// in a spec markdown file resolves correctly both here and on GitHub.
func assetsHandler() http.Handler {
	return http.StripPrefix("/site/", http.FileServer(http.FS(site.Assets)))
}

// landingPageHandler serves the embedded static landing page.
func landingPageHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(site.IndexHTML))
}

// stylesheetHandler serves the embedded stylesheet shared by every page.
func stylesheetHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	_, _ = w.Write([]byte(site.StyleCSS))
}

// noCopyScriptHandler serves the embedded script (blocks right-click,
// selection, and copy/cut outside form fields) shared by every page.
func noCopyScriptHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	_, _ = w.Write([]byte(site.NoCopyJS))
}

// applyPageHandler serves the embedded form where an applicant enters their
// GitHub username; its own JS POSTs to /apply (see router.go/handlers.go).
func applyPageHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(site.ApplyHTML))
}

var challengePageHandler = markdownPage(
	repoRootPath("CHALLENGE.md"),
	"Nexus Transit Authority — Scheduler Challenge",
	challengeResources,
)

var frontendChallengePageHandler = markdownPage(
	repoRootPath("FRONTEND_CHALLENGE.md"),
	"Nexus Transit Authority — Operations Console Challenge",
	frontendChallengeResources,
)
