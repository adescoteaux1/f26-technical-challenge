package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func withFakeGitHub(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	original := apiBaseURL
	apiBaseURL = srv.URL
	t.Cleanup(func() { apiBaseURL = original })

	return NewClient("test-token", "the-org")
}

func TestValidUsername(t *testing.T) {
	cases := map[string]bool{
		"octocat":     true,
		"oct-o-cat":   true,
		"a":           true,
		"-octocat":    false,
		"octocat-":    false,
		"oct--ocat":   false,
		"":            false,
		"has space":   false,
		"under_score": false,
	}
	for username, want := range cases {
		if got := ValidUsername(username); got != want {
			t.Errorf("ValidUsername(%q) = %v, want %v", username, got, want)
		}
	}
}

func TestCreateApplicantRepo_NewRepo(t *testing.T) {
	client := withFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/orgs/the-org/repos":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["name"] != "f26-challenge-jane-doe-octocat" {
				t.Errorf("repo name = %v, want f26-challenge-jane-doe-octocat", body["name"])
			}
			if body["private"] != true {
				t.Errorf("private = %v, want true", body["private"])
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"html_url": "https://github.com/the-org/f26-challenge-jane-doe-octocat",
			})
		case r.Method == http.MethodPut && r.URL.Path == "/repos/the-org/f26-challenge-jane-doe-octocat/collaborators/octocat":
			w.WriteHeader(http.StatusCreated)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})

	repoURL, err := client.CreateApplicantRepo(context.Background(), "octocat", "Jane", "Doe")
	if err != nil {
		t.Fatalf("CreateApplicantRepo() error = %v", err)
	}
	if repoURL != "https://github.com/the-org/f26-challenge-jane-doe-octocat" {
		t.Errorf("repoURL = %q", repoURL)
	}
}

func TestCreateApplicantRepo_ReusesExistingRepo(t *testing.T) {
	client := withFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/orgs/the-org/repos":
			w.WriteHeader(http.StatusUnprocessableEntity)
		case r.Method == http.MethodGet && r.URL.Path == "/repos/the-org/f26-challenge-jane-doe-octocat":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"html_url": "https://github.com/the-org/f26-challenge-jane-doe-octocat",
			})
		case r.Method == http.MethodPut:
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})

	repoURL, err := client.CreateApplicantRepo(context.Background(), "octocat", "Jane", "Doe")
	if err != nil {
		t.Fatalf("CreateApplicantRepo() error = %v", err)
	}
	if repoURL != "https://github.com/the-org/f26-challenge-jane-doe-octocat" {
		t.Errorf("repoURL = %q", repoURL)
	}
}

func TestCreateApplicantRepo_UnknownGitHubUser(t *testing.T) {
	client := withFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"html_url": "https://github.com/the-org/f26-challenge-ghost-ghost",
			})
		case http.MethodPut:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	_, err := client.CreateApplicantRepo(context.Background(), "ghost", "Ghost", "Ghost")
	if err != ErrUserNotFound {
		t.Fatalf("CreateApplicantRepo() error = %v, want ErrUserNotFound", err)
	}
}

func TestCreateApplicantRepo_InvalidUsername(t *testing.T) {
	client := NewClient("token", "the-org")
	_, err := client.CreateApplicantRepo(context.Background(), "-not-valid-", "Jane", "Doe")
	if err != ErrInvalidUsername {
		t.Fatalf("CreateApplicantRepo() error = %v, want ErrInvalidUsername", err)
	}
}

func TestCreateApplicantRepo_MissingName(t *testing.T) {
	client := NewClient("token", "the-org")

	cases := []struct{ first, last string }{
		{"", "Doe"},
		{"Jane", ""},
		{"---", "Doe"},
	}
	for _, c := range cases {
		_, err := client.CreateApplicantRepo(context.Background(), "octocat", c.first, c.last)
		if err != ErrMissingName {
			t.Errorf("CreateApplicantRepo(%q, %q) error = %v, want ErrMissingName", c.first, c.last, err)
		}
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Jane":      "jane",
		"Mary Jane": "mary-jane",
		"O'Brien":   "o-brien",
		"  Doe  ":   "doe",
		"Jean-Luc":  "jean-luc",
		"---":       "",
		"":          "",
	}
	for input, want := range cases {
		if got := slugify(input); got != want {
			t.Errorf("slugify(%q) = %q, want %q", input, got, want)
		}
	}
}
