// Command lookup calls GET /admin/lookup on a Control Tower instance to pull
// up a candidate's registration and expedition history by user ID or email —
// an interviewer's tool, since /me/expeditions only returns the caller's own
// history via their own bearer token.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type expeditionSummary struct {
	ExpeditionID string  `json:"expeditionId"`
	Finished     bool    `json:"finished"`
	OverallScore float64 `json:"overallScore"`
	Metrics      struct {
		Throughput      float64 `json:"throughput"`
		GateUtilization float64 `json:"gateUtilization"`
		ArrivalSuccess  float64 `json:"arrivalSuccess"`
		Fairness        float64 `json:"fairness"`
		Reliability     float64 `json:"reliability"`
		SlaCompliance   float64 `json:"slaCompliance"`
	} `json:"metrics"`
	CreatedAt time.Time `json:"createdAt"`
}

type lookupResponse struct {
	UserID      string              `json:"userId"`
	Email       string              `json:"email"`
	NUID        string              `json:"nuid"`
	CreatedAt   time.Time           `json:"createdAt"`
	Expeditions []expeditionSummary `json:"expeditions"`
}

type problemDetail struct {
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Status int    `json:"status"`
}

func main() {
	var (
		id      = flag.String("id", "", "User ID to look up")
		email   = flag.String("email", "", "Email to look up")
		baseURL = flag.String("base-url", envOr("CONTROL_TOWER_BASE_URL", "https://fall26-challenge.generatenu.com"),
			"Control Tower base URL (env: CONTROL_TOWER_BASE_URL)")
		adminToken = flag.String("token", os.Getenv("ADMIN_TOKEN"), "Admin token (env: ADMIN_TOKEN)")
	)
	flag.Parse()

	if *id == "" && *email == "" {
		fmt.Fprintln(os.Stderr, "usage: lookup --id=<userId> and/or --email=<email> [--base-url=...] [--token=...]")
		os.Exit(2)
	}
	if *adminToken == "" {
		fmt.Fprintln(os.Stderr, "error: an admin token is required (--token or ADMIN_TOKEN env var)")
		os.Exit(2)
	}

	result, err := lookup(*baseURL, *adminToken, *id, *email)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	printResult(result)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func lookup(baseURL, adminToken, id, email string) (*lookupResponse, error) {
	query := url.Values{}
	if id != "" {
		query.Set("id", id)
	}
	if email != "" {
		query.Set("email", email)
	}

	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(baseURL, "/")+"/admin/lookup?"+query.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("X-Admin-Token", adminToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var problem problemDetail
		if json.Unmarshal(body, &problem) == nil && problem.Detail != "" {
			return nil, fmt.Errorf("%s (status %d)", problem.Detail, resp.StatusCode)
		}
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, body)
	}

	var result lookupResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

func printResult(r *lookupResponse) {
	fmt.Printf("User:    %s\n", r.UserID)
	fmt.Printf("Email:   %s\n", r.Email)
	fmt.Printf("NUID:    %s\n", r.NUID)
	fmt.Printf("Since:   %s\n", r.CreatedAt.Format(time.RFC3339))
	fmt.Println()

	if len(r.Expeditions) == 0 {
		fmt.Println("No expeditions on file.")
		return
	}

	fmt.Printf("%d expedition(s):\n\n", len(r.Expeditions))
	for _, e := range r.Expeditions {
		status := "in progress"
		if e.Finished {
			status = fmt.Sprintf("finished, overallScore=%.1f", e.OverallScore)
		}
		fmt.Printf("- %s  (%s)  started %s\n", e.ExpeditionID, status, e.CreatedAt.Format(time.RFC3339))
		if e.Finished {
			fmt.Printf("    throughput=%.1f gateUtilization=%.1f arrivalSuccess=%.1f fairness=%.1f reliability=%.1f slaCompliance=%.1f\n",
				e.Metrics.Throughput, e.Metrics.GateUtilization, e.Metrics.ArrivalSuccess,
				e.Metrics.Fairness, e.Metrics.Reliability, e.Metrics.SlaCompliance)
		}
	}
}
