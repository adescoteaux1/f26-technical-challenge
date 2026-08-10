// Package github is a thin client for the one GitHub REST API workflow this
// project needs: given an applicant's username, create (or reuse) a private
// repo under the org and add them as a push collaborator, so they can start
// the challenge without creating and sharing their own repo.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
)

var (
	ErrInvalidUsername = errors.New("invalid GitHub username")
	ErrUserNotFound    = errors.New("GitHub user not found")
)

// apiBaseURL is overridden in tests to point at a fake GitHub server.
var apiBaseURL = "https://api.github.com"

// usernamePattern mirrors GitHub's own username rules: alphanumeric
// characters or single hyphens, never leading/trailing/doubled, max 39 chars.
var usernamePattern = regexp.MustCompile(`^[a-zA-Z\d](?:[a-zA-Z\d]|-(?:[a-zA-Z\d])){0,38}$`)

func ValidUsername(username string) bool {
	return usernamePattern.MatchString(username)
}

// Client talks to the GitHub REST API as whatever identity Token belongs to.
// That identity needs "Create repository" and "Members: write" permission
// (or equivalent classic PAT scopes) on Org.
type Client struct {
	Token      string
	Org        string
	HTTPClient *http.Client
}

func NewClient(token, org string) *Client {
	return &Client{Token: token, Org: org, HTTPClient: http.DefaultClient}
}

// CreateApplicantRepo creates a private repo under c.Org named for username
// (reusing it if this applicant already has one), invites username as a push
// collaborator, and returns the repo's HTML URL.
func (c *Client) CreateApplicantRepo(ctx context.Context, username string) (string, error) {
	if !ValidUsername(username) {
		return "", ErrInvalidUsername
	}

	repoName := "challenge-" + username
	repoURL, err := c.createOrGetRepo(ctx, repoName)
	if err != nil {
		return "", err
	}

	if err := c.inviteCollaborator(ctx, repoName, username); err != nil {
		return "", err
	}

	return repoURL, nil
}

func (c *Client) createOrGetRepo(ctx context.Context, repoName string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"name":        repoName,
		"private":     true,
		"description": "F26 technical challenge submission",
	})

	req, err := c.newRequest(ctx, http.MethodPost, fmt.Sprintf("/orgs/%s/repos", c.Org), body)
	if err != nil {
		return "", err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("create repo: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusCreated:
		return decodeHTMLURL(resp)
	case http.StatusUnprocessableEntity:
		// Name already exists under the org — this applicant already has a
		// repo from a prior submission. Treat as idempotent, not an error.
		return c.getRepo(ctx, repoName)
	default:
		return "", fmt.Errorf("create repo: unexpected status %d: %s", resp.StatusCode, readBody(resp))
	}
}

func (c *Client) getRepo(ctx context.Context, repoName string) (string, error) {
	req, err := c.newRequest(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/%s", c.Org, repoName), nil)
	if err != nil {
		return "", err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("get repo: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("get repo: unexpected status %d: %s", resp.StatusCode, readBody(resp))
	}
	return decodeHTMLURL(resp)
}

func (c *Client) inviteCollaborator(ctx context.Context, repoName, username string) error {
	body, _ := json.Marshal(map[string]string{"permission": "push"})

	req, err := c.newRequest(ctx, http.MethodPut,
		fmt.Sprintf("/repos/%s/%s/collaborators/%s", c.Org, repoName, username), body)
	if err != nil {
		return err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("invite collaborator: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusCreated, http.StatusNoContent:
		// 201 = new invitation sent, 204 = username is already a collaborator.
		return nil
	case http.StatusNotFound:
		return ErrUserNotFound
	default:
		return fmt.Errorf("invite collaborator: unexpected status %d: %s", resp.StatusCode, readBody(resp))
	}
}

func (c *Client) newRequest(ctx context.Context, method, path string, body []byte) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, apiBaseURL+path, reader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func decodeHTMLURL(resp *http.Response) (string, error) {
	var out struct {
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	return out.HTMLURL, nil
}

func readBody(resp *http.Response) string {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	return string(b)
}
