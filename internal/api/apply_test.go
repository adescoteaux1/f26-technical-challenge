package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/adescoteaux1/generate-control-tower/internal/github"
	"github.com/adescoteaux1/generate-control-tower/internal/storetest"
)

// fakeProvisioner is a stand-in for *github.Client so these tests don't hit
// real GitHub — they exercise applyHandler's response mapping only.
type fakeProvisioner struct {
	repoURL string
	err     error
	calls   []string
}

func (f *fakeProvisioner) CreateApplicantRepo(_ context.Context, username string) (string, error) {
	f.calls = append(f.calls, username)
	return f.repoURL, f.err
}

func newTestServerWithGitHub(gh applicantRepoProvisioner) *httptest.Server {
	s := &Server{Store: storetest.New(), Log: slog.New(slog.NewTextHandler(io.Discard, nil)), GitHub: gh}
	return httptest.NewServer(NewRouter(s))
}

func TestApply_Success(t *testing.T) {
	fake := &fakeProvisioner{repoURL: "https://github.com/the-org/challenge-octocat"}
	srv := newTestServerWithGitHub(fake)
	defer srv.Close()

	resp, body := doJSON(t, http.MethodPost, srv.URL+"/apply", "", map[string]string{
		"githubUsername": "octocat",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, body = %v", resp.StatusCode, body)
	}
	if body["repoUrl"] != fake.repoURL {
		t.Errorf("repoUrl = %v, want %v", body["repoUrl"], fake.repoURL)
	}
	if len(fake.calls) != 1 || fake.calls[0] != "octocat" {
		t.Errorf("calls = %v", fake.calls)
	}
}

func TestApply_UnknownGitHubUser(t *testing.T) {
	fake := &fakeProvisioner{err: github.ErrUserNotFound}
	srv := newTestServerWithGitHub(fake)
	defer srv.Close()

	resp, _ := doJSON(t, http.MethodPost, srv.URL+"/apply", "", map[string]string{
		"githubUsername": "ghost",
	})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestApply_InvalidUsername(t *testing.T) {
	fake := &fakeProvisioner{err: github.ErrInvalidUsername}
	srv := newTestServerWithGitHub(fake)
	defer srv.Close()

	resp, _ := doJSON(t, http.MethodPost, srv.URL+"/apply", "", map[string]string{
		"githubUsername": "-nope-",
	})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
}

func TestApply_NotConfigured(t *testing.T) {
	srv := newTestServer() // no GitHub client set
	defer srv.Close()

	resp, _ := doJSON(t, http.MethodPost, srv.URL+"/apply", "", map[string]string{
		"githubUsername": "octocat",
	})
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

func TestApply_NoAuthRequired(t *testing.T) {
	fake := &fakeProvisioner{repoURL: "https://github.com/the-org/challenge-octocat"}
	srv := newTestServerWithGitHub(fake)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/apply", nil)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		t.Fatalf("status = 401, /apply should not require a bearer token")
	}
}
