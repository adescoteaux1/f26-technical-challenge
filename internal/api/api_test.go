package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/adescoteaux1/generate-oracle/internal/storetest"
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

func TestCreateEvaluation_RequiresAuth(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	resp, body := doJSON(t, http.MethodPost, srv.URL+"/evaluation", "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without a token, got %d (%v)", resp.StatusCode, body)
	}
}

func TestRegisterLoginAndRunEvaluationFlow(t *testing.T) {
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

	// Create an evaluation using the fresh token.
	resp, createBody := doJSON(t, http.MethodPost, srv.URL+"/evaluation", token, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 creating evaluation, got %d (%v)", resp.StatusCode, createBody)
	}
	evalID, _ := createBody["evaluationId"].(string)
	if evalID == "" {
		t.Fatalf("expected evaluationId in response, got %v", createBody)
	}

	// GET current state with the same token should succeed.
	resp, stateBody := doJSON(t, http.MethodGet, srv.URL+"/evaluation/"+evalID, token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 fetching evaluation state, got %d (%v)", resp.StatusCode, stateBody)
	}

	// History should now include this evaluation.
	histReq, err := http.NewRequest(http.MethodGet, srv.URL+"/me/evaluations", nil)
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
	if len(history) != 1 || history[0]["evaluationId"] != evalID {
		t.Fatalf("expected history to contain the new evaluation, got %v", history)
	}
}

func TestOwnership_OtherUserCannotAccessEvaluation(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	tokenA := registerUser(t, srv.URL, "a@example.com")
	tokenB := registerUser(t, srv.URL, "b@example.com")

	_, createBody := doJSON(t, http.MethodPost, srv.URL+"/evaluation", tokenA, nil)
	evalID := createBody["evaluationId"].(string)

	resp, body := doJSON(t, http.MethodGet, srv.URL+"/evaluation/"+evalID, tokenB, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for a non-owner, got %d (%v)", resp.StatusCode, body)
	}

	resp, body = doJSON(t, http.MethodPost, srv.URL+"/simulation/"+evalID+"/schedule", tokenB, []any{})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 scheduling as a non-owner, got %d (%v)", resp.StatusCode, body)
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
