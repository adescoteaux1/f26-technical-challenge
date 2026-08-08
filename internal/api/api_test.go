package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/adescoteaux1/generate-control-tower/internal/storetest"
)

func newTestServer() *httptest.Server {
	s := &Server{Store: storetest.New(), Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	return httptest.NewServer(NewRouter(s))
}

func doJSON(t *testing.T, method, url, token string, body any) (*http.Response, map[string]any) {
	t.Helper()

	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	var parsed map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&parsed)
	return resp, parsed
}

func registerUser(t *testing.T, baseURL, email string) string {
	t.Helper()
	resp, body := doJSON(t, http.MethodPost, baseURL+"/register", "", map[string]string{
		"email": email, "nuid": "001234567",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register failed: status=%d body=%v", resp.StatusCode, body)
	}
	token, _ := body["token"].(string)
	if token == "" {
		t.Fatalf("expected a token in register response, got %v", body)
	}
	return token
}

func TestCreateExpedition_RequiresAuth(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	resp, body := doJSON(t, http.MethodPost, srv.URL+"/expedition", "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without a token, got %d (%v)", resp.StatusCode, body)
	}
}

func TestRegisterLoginAndRunExpeditionFlow(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	token := registerUser(t, srv.URL, "ally@example.com")

	// Duplicate registration should fail.
	resp, _ := doJSON(t, http.MethodPost, srv.URL+"/register", "", map[string]string{
		"email": "ally@example.com", "nuid": "001234567",
	})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 for duplicate email, got %d", resp.StatusCode)
	}

	// Login re-authenticates and rotates the token.
	resp, loginBody := doJSON(t, http.MethodPost, srv.URL+"/login", "", map[string]string{
		"email": "ally@example.com", "nuid": "001234567",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from login, got %d", resp.StatusCode)
	}
	token = loginBody["token"].(string)

	// Create an expedition using the fresh token.
	resp, createBody := doJSON(t, http.MethodPost, srv.URL+"/expedition", token, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 creating expedition, got %d (%v)", resp.StatusCode, createBody)
	}
	expID, _ := createBody["expeditionId"].(string)
	if expID == "" {
		t.Fatalf("expected expeditionId in response, got %v", createBody)
	}

	// GET current state with the same token should succeed.
	resp, stateBody := doJSON(t, http.MethodGet, srv.URL+"/expedition/"+expID, token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 fetching expedition state, got %d (%v)", resp.StatusCode, stateBody)
	}

	// History should now include this expedition.
	histReq, err := http.NewRequest(http.MethodGet, srv.URL+"/me/expeditions", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	histReq.Header.Set("Authorization", "Bearer "+token)
	histResp, err := http.DefaultClient.Do(histReq)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer histResp.Body.Close()
	var history []map[string]any
	if err := json.NewDecoder(histResp.Body).Decode(&history); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(history) != 1 || history[0]["expeditionId"] != expID {
		t.Fatalf("expected history to contain the new expedition, got %v", history)
	}
}

func TestOwnership_OtherUserCannotAccessExpedition(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	tokenA := registerUser(t, srv.URL, "a@example.com")
	tokenB := registerUser(t, srv.URL, "b@example.com")

	_, createBody := doJSON(t, http.MethodPost, srv.URL+"/expedition", tokenA, nil)
	expID := createBody["expeditionId"].(string)

	resp, body := doJSON(t, http.MethodGet, srv.URL+"/expedition/"+expID, tokenB, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for a non-owner, got %d (%v)", resp.StatusCode, body)
	}

	resp, body = doJSON(t, http.MethodPost, srv.URL+"/cycle/"+expID+"/schedule", tokenB, []any{})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 scheduling as a non-owner, got %d (%v)", resp.StatusCode, body)
	}
}

func TestChaosProbe_DefaultsToSuccess(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()
	token := registerUser(t, srv.URL, "chaos-default@example.com")

	resp, body := doJSON(t, http.MethodGet, srv.URL+"/chaos/probe", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 with no query params, got %d (%v)", resp.StatusCode, body)
	}
	if body["outcome"] != "success" {
		t.Fatalf("expected outcome success, got %v", body)
	}
}

func TestChaosProbe_RequiresAuth(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	resp, body := doJSON(t, http.MethodGet, srv.URL+"/chaos/probe", "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without a token, got %d (%v)", resp.StatusCode, body)
	}
}

func TestChaosProbe_ErrorModeAlwaysFails(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()
	token := registerUser(t, srv.URL, "chaos-error@example.com")

	resp, body := doJSON(t, http.MethodGet, srv.URL+"/chaos/probe?mode=error", token, nil)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 for mode=error, got %d (%v)", resp.StatusCode, body)
	}
}

func TestChaosProbe_FlakyModeIsDeterministic(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()
	token := registerUser(t, srv.URL, "chaos-flaky@example.com")

	resp, body := doJSON(t, http.MethodGet, srv.URL+"/chaos/probe?mode=flaky&attempt=1&failUntil=3", token, nil)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 on attempt 1 of 3, got %d (%v)", resp.StatusCode, body)
	}

	resp, body = doJSON(t, http.MethodGet, srv.URL+"/chaos/probe?mode=flaky&attempt=2&failUntil=3", token, nil)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 on attempt 2 of 3, got %d (%v)", resp.StatusCode, body)
	}

	resp, body = doJSON(t, http.MethodGet, srv.URL+"/chaos/probe?mode=flaky&attempt=3&failUntil=3", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on attempt 3 of 3, got %d (%v)", resp.StatusCode, body)
	}
	if body["outcome"] != "success" {
		t.Fatalf("expected outcome success once attempt reaches failUntil, got %v", body)
	}
}

func TestLandingPage_Serves(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Nexus Transit Authority") {
		t.Error("expected landing page to mention Nexus Transit Authority")
	}
	if !strings.Contains(string(body), `href="/challenge"`) {
		t.Error("expected landing page to link to /challenge")
	}
}

func TestStylesheet_Serves(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/style.css")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Errorf("expected text/css content type, got %q", ct)
	}
}

func TestChallengePage_RendersMarkdownWithTablesAndResources(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/challenge")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	html := string(body)
	if !strings.Contains(html, "<table>") {
		t.Error("expected CHALLENGE.md's markdown tables (e.g. the rubric) to render as <table>")
	}
	if !strings.Contains(html, "Resources") {
		t.Error("expected a Resources section")
	}
	if !strings.Contains(html, `href="/docs"`) {
		t.Error("expected a link to the Scalar API reference at /docs")
	}
}

func TestFrontendChallengePage_RendersPlaceholderWithoutResourcesSection(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/frontend-challenge")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	html := string(body)
	if !strings.Contains(html, "placeholder") {
		t.Error("expected FRONTEND_CHALLENGE.md's placeholder text to render")
	}
	if strings.Contains(html, "<h2>Resources</h2>") {
		t.Error("expected no Resources section while there's no curated list for this page yet")
	}
}

func TestLogin_RejectsUnknownUser(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	resp, _ := doJSON(t, http.MethodPost, srv.URL+"/login", "", map[string]string{
		"email": "nobody@example.com", "nuid": "001234567",
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unknown user, got %d", resp.StatusCode)
	}
}
