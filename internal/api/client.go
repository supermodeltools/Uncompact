package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/supermodeltools/uncompact/internal/config"
)

const (
	defaultTimeout    = 60 * time.Second
	pollInterval      = 2 * time.Second
	maxPollDuration   = 120 * time.Second
)

// Client is the Supermodel API client.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	debug      bool
	logFn      func(format string, args ...interface{})
}

// GraphResponse is the response from the /v1/graphs/supermodel endpoint.
type GraphResponse struct {
	ID       string          `json:"id"`
	Status   string          `json:"status"` // "pending", "processing", "complete", "error"
	Project  *ProjectGraph   `json:"project,omitempty"`
	Error    string          `json:"error,omitempty"`
}

// ProjectGraph holds the analyzed project graph from Supermodel.
type ProjectGraph struct {
	Name        string    `json:"name"`
	Language    string    `json:"language"`
	Framework   string    `json:"framework,omitempty"`
	Description string    `json:"description,omitempty"`
	Domains     []Domain  `json:"domains"`
	Stats       Stats     `json:"stats"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Domain represents a semantic domain within the project.
type Domain struct {
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	KeyFiles      []string `json:"key_files"`
	Responsibilities []string `json:"responsibilities"`
	Subdomains    []string `json:"subdomains,omitempty"`
	DependsOn     []string `json:"depends_on,omitempty"`
}

// Stats holds codebase statistics.
type Stats struct {
	TotalFiles     int    `json:"total_files"`
	TotalFunctions int    `json:"total_functions"`
	TotalLines     int    `json:"total_lines"`
	Languages      map[string]int `json:"languages,omitempty"`
}

// JobStatus is the response from the async job polling endpoint.
type JobStatus struct {
	JobID   string       `json:"job_id"`
	Status  string       `json:"status"` // "pending", "processing", "complete", "error"
	Graph   *ProjectGraph `json:"graph,omitempty"`
	Error   string       `json:"error,omitempty"`
}

// New creates a new API client.
func New(baseURL, apiKey string, debug bool, logFn func(string, ...interface{})) *Client {
	if logFn == nil {
		logFn = func(string, ...interface{}) {}
	}
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		debug:   debug,
		logFn:   logFn,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
	}
}

// GetGraph submits the repo zip and retrieves the project graph, handling async polling.
func (c *Client) GetGraph(ctx context.Context, projectName string, repoZip []byte) (*ProjectGraph, error) {
	c.logFn("[debug] submitting repo to Supermodel API (%d bytes)", len(repoZip))

	// Build multipart body
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)

	_ = mw.WriteField("project_name", projectName)

	fw, err := mw.CreateFormFile("repo", "repo.zip")
	if err != nil {
		return nil, fmt.Errorf("creating multipart field: %w", err)
	}
	if _, err := fw.Write(repoZip); err != nil {
		return nil, fmt.Errorf("writing zip: %w", err)
	}
	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("closing multipart: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/v1/graphs/supermodel", &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "uncompact/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	c.logFn("[debug] API response status: %d", resp.StatusCode)

	switch resp.StatusCode {
	case http.StatusOK:
		// Synchronous response — graph is ready immediately
		var graph ProjectGraph
		if err := json.Unmarshal(respBody, &graph); err != nil {
			return nil, fmt.Errorf("parsing graph response: %w", err)
		}
		return &graph, nil

	case http.StatusAccepted:
		// Async response — poll for completion
		var jobResp struct {
			JobID string `json:"job_id"`
		}
		if err := json.Unmarshal(respBody, &jobResp); err != nil {
			return nil, fmt.Errorf("parsing async response: %w", err)
		}
		c.logFn("[debug] async job submitted: %s", jobResp.JobID)
		return c.pollJob(ctx, jobResp.JobID)

	case http.StatusUnauthorized:
		return nil, fmt.Errorf("authentication failed: check your API key at %s", config.DashboardURL)

	case http.StatusPaymentRequired:
		return nil, fmt.Errorf("subscription required: visit %s to subscribe", config.DashboardURL)

	case http.StatusTooManyRequests:
		return nil, fmt.Errorf("rate limit exceeded: please wait before retrying")

	default:
		var errResp struct {
			Message string `json:"message"`
			Error   string `json:"error"`
		}
		_ = json.Unmarshal(respBody, &errResp)
		msg := errResp.Message
		if msg == "" {
			msg = errResp.Error
		}
		if msg == "" {
			msg = string(respBody)
		}
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, msg)
	}
}

// pollJob polls the async job endpoint until completion or timeout.
func (c *Client) pollJob(ctx context.Context, jobID string) (*ProjectGraph, error) {
	deadline := time.Now().Add(maxPollDuration)

	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("job %s timed out after %v", jobID, maxPollDuration)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}

		c.logFn("[debug] polling job %s", jobID)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet,
			c.baseURL+"/v1/graphs/supermodel/"+jobID, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "uncompact/1.0")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			c.logFn("[debug] poll error (will retry): %v", err)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			continue
		}

		var status JobStatus
		if err := json.Unmarshal(body, &status); err != nil {
			continue
		}

		c.logFn("[debug] job status: %s", status.Status)

		switch status.Status {
		case "complete":
			if status.Graph == nil {
				return nil, fmt.Errorf("job complete but no graph data returned")
			}
			return status.Graph, nil
		case "error":
			return nil, fmt.Errorf("API job failed: %s", status.Error)
		case "pending", "processing":
			// continue polling
		default:
			c.logFn("[debug] unknown job status: %s", status.Status)
		}
	}
}

// ValidateKey checks if the API key is valid by calling the auth endpoint.
func (c *Client) ValidateKey(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/v1/auth/me", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "uncompact/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("auth check failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return "", fmt.Errorf("invalid API key")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("auth check failed with status %d", resp.StatusCode)
	}

	var me struct {
		Email string `json:"email"`
		Plan  string `json:"plan"`
	}
	if err := json.Unmarshal(body, &me); err != nil {
		return "", fmt.Errorf("parsing auth response: %w", err)
	}
	return fmt.Sprintf("%s (%s)", me.Email, me.Plan), nil
}
