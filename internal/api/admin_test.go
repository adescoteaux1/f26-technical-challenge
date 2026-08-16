package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/adescoteaux1/generate-control-tower/internal/storetest"
)

func newTestServerWithAdmin(adminToken string) *httptest.Server {
	s := &Server{Store: storetest.New(), Log: slog.New(slog.NewTextHandler(io.Discard, nil)), AdminToken: adminToken}
	return httptest.NewServer(NewRouter(s))
}

func adminLookup(t *testing.T, baseURL, adminToken, query string) (*http.Response, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, baseURL+"/admin/lookup?"+query, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if adminToken != "" {
		req.Header.Set("X-Admin-Token", adminToken)
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

func TestAdminLookup_ByID(t *testing.T) {
	srv := newTestServerWithAdmin("secret")
	defer srv.Close()

	_, regBody := doJSON(t, http.MethodPost, srv.URL+"/register", "", map[string]string{
		"email": "candidate@example.com", "nuid": "001234567",
	})
	userID, _ := regBody["userId"].(string)
	if userID == "" {
		t.Fatalf("expected userId in register response, got %v", regBody)
	}

	resp, body := adminLookup(t, srv.URL, "secret", "id="+userID)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %v", resp.StatusCode, body)
	}
	if body["userId"] != userID {
		t.Errorf("userId = %v, want %v", body["userId"], userID)
	}
	if body["email"] != "candidate@example.com" {
		t.Errorf("email = %v", body["email"])
	}
	if _, ok := body["expeditions"].([]any); !ok {
		t.Errorf("expeditions = %v, want an array", body["expeditions"])
	}
}

func TestAdminLookup_ByEmail(t *testing.T) {
	srv := newTestServerWithAdmin("secret")
	defer srv.Close()

	doJSON(t, http.MethodPost, srv.URL+"/register", "", map[string]string{
		"email": "candidate2@example.com", "nuid": "001234567",
	})

	resp, body := adminLookup(t, srv.URL, "secret", "email=candidate2@example.com")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %v", resp.StatusCode, body)
	}
	if body["email"] != "candidate2@example.com" {
		t.Errorf("email = %v", body["email"])
	}
}

func TestAdminLookup_WrongToken(t *testing.T) {
	srv := newTestServerWithAdmin("secret")
	defer srv.Close()

	resp, _ := adminLookup(t, srv.URL, "wrong", "email=candidate@example.com")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestAdminLookup_MissingToken(t *testing.T) {
	srv := newTestServerWithAdmin("secret")
	defer srv.Close()

	resp, _ := adminLookup(t, srv.URL, "", "email=candidate@example.com")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestAdminLookup_NotConfigured(t *testing.T) {
	srv := newTestServer() // no AdminToken set
	defer srv.Close()

	resp, _ := adminLookup(t, srv.URL, "anything", "email=candidate@example.com")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

func TestAdminLookup_MissingIdentifiers(t *testing.T) {
	srv := newTestServerWithAdmin("secret")
	defer srv.Close()

	resp, _ := adminLookup(t, srv.URL, "secret", "")
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
}

func TestAdminLookup_NotFound(t *testing.T) {
	srv := newTestServerWithAdmin("secret")
	defer srv.Close()

	resp, _ := adminLookup(t, srv.URL, "secret", "email=nobody@example.com")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}
