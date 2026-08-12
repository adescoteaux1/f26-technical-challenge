package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/adescoteaux1/generate-control-tower/internal/models"
	"github.com/adescoteaux1/generate-control-tower/internal/portals"
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

func TestPortalNetworkStatus_ReturnsSixPortalsWithDerivedStatus(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/frontend/portals")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 without a token, got %d", resp.StatusCode)
	}

	var body []portalStatusItem
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode portals: %v", err)
	}
	if len(body) != 6 {
		t.Fatalf("expected exactly 6 portals, got %d (%v)", len(body), body)
	}
	for i, p := range body {
		if p.Name != portals.Names[i] {
			t.Errorf("portal %d: name = %q, want %q", i, p.Name, portals.Names[i])
		}
		if p.Load < 0 || p.Load > 100 {
			t.Errorf("%s: load %d out of range", p.Name, p.Load)
		}
		switch p.Status {
		case portals.StatusOffline:
			if p.Load != 0 {
				t.Errorf("%s: offline but reports load %d", p.Name, p.Load)
			}
		case portals.StatusUnstable:
			if p.Load < portals.UnstableLoadThreshold {
				t.Errorf("%s: unstable but reports load %d, expected %d or above",
					p.Name, p.Load, portals.UnstableLoadThreshold)
			}
		case portals.StatusNominal:
			if p.Load >= portals.UnstableLoadThreshold {
				t.Errorf("%s: nominal but reports load %d, expected below %d",
					p.Name, p.Load, portals.UnstableLoadThreshold)
			}
		default:
			t.Errorf("%s: unexpected status %q", p.Name, p.Status)
		}
	}
}

// defaultBookingLimit mirrors the `default` struct tag on bookingsInput.Limit,
// which a struct tag can't reference directly.
const defaultBookingLimit = 10

func getBookings(t *testing.T, baseURL, query string) (*http.Response, bookingsPage) {
	t.Helper()

	resp, err := http.Get(baseURL + "/frontend/bookings" + query)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var page bookingsPage
	_ = json.NewDecoder(resp.Body).Decode(&page)
	return resp, page
}

func TestBookings_FirstBatchDefaultsToTenWithACursor(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	resp, page := getBookings(t, srv.URL, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 without a token, got %d", resp.StatusCode)
	}
	if len(page.Items) != defaultBookingLimit {
		t.Errorf("expected %d items by default, got %d", defaultBookingLimit, len(page.Items))
	}
	if page.Total <= len(page.Items) {
		t.Errorf("expected total (%d) to exceed one batch of %d", page.Total, len(page.Items))
	}
	if !page.HasMore {
		t.Error("expected hasMore on the first of several batches")
	}
	if page.NextCursor == "" {
		t.Error("expected a nextCursor while hasMore is true")
	}
}

func TestBookings_RejectsLimitAboveTheCap(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	resp, _ := getBookings(t, srv.URL, "?limit=100")
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for an over-sized limit, got %d", resp.StatusCode)
	}
}

func TestBookings_RejectsMalformedCursor(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	for _, cursor := range []string{"not-base64!!", "bm90LWEtY3Vyc29y", "MjAyNi0wMS0wMXxCSy0x"} {
		resp, _ := getBookings(t, srv.URL, "?cursor="+cursor)
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("cursor %q: expected 422, got %d", cursor, resp.StatusCode)
		}
	}
}

func TestBookings_WalksEveryBookingExactlyOnce(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	seen := map[string]bool{}
	var total, batches int

	query := ""
	for {
		resp, page := getBookings(t, srv.URL, query)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("batch %d: expected 200, got %d", batches, resp.StatusCode)
		}
		total = page.Total
		for _, b := range page.Items {
			if seen[b.Reference] {
				t.Errorf("batch %d: %s was returned twice", batches, b.Reference)
			}
			seen[b.Reference] = true
		}

		batches++
		if !page.HasMore {
			if page.NextCursor != "" {
				t.Error("expected no nextCursor on the final batch")
			}
			break
		}
		if page.NextCursor == "" {
			t.Fatalf("batch %d: hasMore is true but no cursor to continue from", batches)
		}
		if batches > 50 {
			t.Fatal("hasMore never went false; the scroll does not terminate")
		}
		query = "?cursor=" + page.NextCursor
	}

	if len(seen) != total {
		t.Errorf("walked %d distinct bookings across %d batches, but total says %d", len(seen), batches, total)
	}
}

func TestBookings_ExhaustedCursorReturnsEmptyBatch(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	// Walk to the final batch, then continue from its last item.
	var last bookingItem
	query := ""
	for {
		_, page := getBookings(t, srv.URL, query)
		if len(page.Items) > 0 {
			last = page.Items[len(page.Items)-1]
		}
		if !page.HasMore {
			break
		}
		query = "?cursor=" + page.NextCursor
	}

	beyond := base64.RawURLEncoding.EncodeToString(
		[]byte(last.DepartsAt.UTC().Format(time.RFC3339Nano) + "|" + last.Reference))
	resp, page := getBookings(t, srv.URL, "?cursor="+beyond)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 past the end, got %d", resp.StatusCode)
	}
	if len(page.Items) != 0 {
		t.Errorf("expected no items past the last booking, got %d", len(page.Items))
	}
	if page.HasMore || page.NextCursor != "" {
		t.Error("expected hasMore false and no cursor past the end")
	}
	if page.Total == 0 {
		t.Error("expected total to still report the real count past the end")
	}
}

func TestBookings_LimitOfOneStillWalksEverything(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	seen := map[string]bool{}
	query := "?limit=1"
	for {
		_, page := getBookings(t, srv.URL, query)
		if len(page.Items) > 1 {
			t.Fatalf("expected at most 1 item with limit=1, got %d", len(page.Items))
		}
		for _, b := range page.Items {
			seen[b.Reference] = true
		}
		if !page.HasMore {
			if len(seen) != page.Total {
				t.Errorf("limit=1 walked %d bookings, total says %d", len(seen), page.Total)
			}
			return
		}
		query = "?limit=1&cursor=" + page.NextCursor
	}
}

func TestBookings_ItemsAreOrderedAndInternallyConsistent(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	_, page := getBookings(t, srv.URL, "")
	for i, b := range page.Items {
		if b.Reference == "" || b.Destination == "" || b.Portal == "" {
			t.Errorf("item %d: missing a required field: %+v", i, b)
		}
		if i > 0 && b.DepartsAt.Before(page.Items[i-1].DepartsAt) {
			t.Errorf("item %d departs before the one before it; expected soonest first", i)
		}
		switch b.Status {
		case string(models.BookingHeld):
			if b.Load != nil {
				t.Errorf("%s: held bookings have no load reading, got %d", b.Reference, *b.Load)
			}
			if b.StatusDetail == "" {
				t.Errorf("%s: held bookings carry a reason line", b.Reference)
			}
		case string(models.BookingQueued):
			if b.StatusDetail == "" {
				t.Errorf("%s: queued bookings carry a reason line", b.Reference)
			}
		case string(models.BookingCleared), string(models.BookingCanceled):
		default:
			t.Errorf("%s: unexpected status %q", b.Reference, b.Status)
		}
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
