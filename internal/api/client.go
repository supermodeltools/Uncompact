package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/supermodeltools/uncompact/internal/config"
)

const (
	defaultTimeout  = 30 * time.Second
	maxPollDuration = 900 * time.Second
	maxPollAttempts = 90
	maxResponseSize = 32 * 1024 * 1024 // 32 MB
)

// Client is the Supermodel API client.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	debug      bool
	logFn      func(format string, args ...interface{})
}

// SupermodelIR is the raw response from the Supermodel API /v1/graphs/supermodel endpoint.
type SupermodelIR struct {
	Repo     string         `json:"repo"`
	Summary  map[string]any `json:"summary"`
	Metadata irMetadata     `json:"metadata"`
	Domains  []irDomain     `json:"domains"`
	Graph    irGraph        `json:"graph"`
}

type irGraph struct {
	Nodes         []irNode         `json:"nodes"`
	Relationships []irRelationship `json:"relationships"`
}

type irNode struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

type irRelationship struct {
	Type   string `json:"type"`
	Source string `json:"source"`
	Target string `json:"target"`
}

type irMetadata struct {
	FileCount int      `json:"fileCount"`
	Languages []string `json:"languages"`
}

type irDomain struct {
	Name               string        `json:"name"`
	DescriptionSummary string        `json:"descriptionSummary"`
	KeyFiles           []string      `json:"keyFiles"`
	Responsibilities   []string      `json:"responsibilities"`
	Subdomains         []irSubdomain `json:"subdomains"`
}

type irSubdomain struct {
	Name               string `json:"name"`
	DescriptionSummary string `json:"descriptionSummary"`
}

// computeCriticalFiles derives the most-connected files by counting how many domains
// reference each file as a key file. The top n files are returned, ranked descending.
func computeCriticalFiles(domains []Domain, n int) []CriticalFile {
	if n <= 0 {
		return nil
	}
	counts := make(map[string]int)
	for _, d := range domains {
		seen := make(map[string]struct{}, len(d.KeyFiles))
		for _, f := range d.KeyFiles {
			if _, exists := seen[f]; exists {
				continue
			}
			seen[f] = struct{}{}
			counts[f]++
		}
	}

	files := make([]CriticalFile, 0, len(counts))
	for path, count := range counts {
		files = append(files, CriticalFile{Path: path, RelationshipCount: count})
	}

	sort.Slice(files, func(i, j int) bool {
		if files[i].RelationshipCount != files[j].RelationshipCount {
			return files[i].RelationshipCount > files[j].RelationshipCount
		}
		return files[i].Path < files[j].Path
	})

	if len(files) > n {
		files = files[:n]
	}
	return files
}

// toProjectGraph converts a SupermodelIR API response into the internal ProjectGraph model.
func (ir *SupermodelIR) toProjectGraph(projectName string) *ProjectGraph {
	lang := ""
	if len(ir.Metadata.Languages) > 0 {
		lang = ir.Metadata.Languages[0]
	}
	if v, ok := ir.Summary["primaryLanguage"]; ok && v != nil {
		if s, ok := v.(string); ok && s != "" {
			lang = s
		}
	}

	// Extract integer fields from the free-form summary map.
	// JSON numbers unmarshal as float64 in map[string]any.
	summaryInt := func(key string) int {
		if v, ok := ir.Summary[key]; ok {
			if n, ok := v.(float64); ok {
				return int(n)
			}
		}
		return 0
	}

	// Build a map of domain → []dependsOn from DOMAIN_RELATES edges.
	dependsOnMap := make(map[string][]string)
	for _, rel := range ir.Graph.Relationships {
		if rel.Type == "DOMAIN_RELATES" && rel.Source != "" && rel.Target != "" {
			dependsOnMap[rel.Source] = append(dependsOnMap[rel.Source], rel.Target)
		}
	}

	domains := make([]Domain, 0, len(ir.Domains))
	for _, d := range ir.Domains {
		subdomains := make([]Subdomain, 0, len(d.Subdomains))
		for _, s := range d.Subdomains {
			subdomains = append(subdomains, Subdomain{
				Name:        s.Name,
				Description: s.DescriptionSummary,
			})
		}
		domains = append(domains, Domain{
			Name:             d.Name,
			Description:      d.DescriptionSummary,
			KeyFiles:         d.KeyFiles,
			Responsibilities: d.Responsibilities,
			Subdomains:       subdomains,
			DependsOn:        dependsOnMap[d.Name],
		})
	}

	var externalDeps []string
	for _, node := range ir.Graph.Nodes {
		if node.Type == "ExternalDependency" && node.Name != "" {
			externalDeps = append(externalDeps, node.Name)
		}
	}

	graph := &ProjectGraph{
		Name:         projectName,
		Language:     lang,
		Domains:      domains,
		ExternalDeps: externalDeps,
		Stats: Stats{
			TotalFiles:     summaryInt("filesProcessed"),
			TotalFunctions: summaryInt("functions"),
			Languages:      ir.Metadata.Languages,
		},
		UpdatedAt: time.Now(),
	}
	graph.CriticalFiles = computeCriticalFiles(graph.Domains, 10)
	return graph
}

// CriticalFile represents a highly-connected file derived from domain key file references.
type CriticalFile struct {
	Path              string `json:"path"`
	RelationshipCount int    `json:"relationship_count"`
}

// ProjectGraph is the internal model used by the cache and template.
type ProjectGraph struct {
	Name          string                   `json:"name"`
	Language      string                   `json:"language"`
	Framework     string                   `json:"framework,omitempty"`
	Description   string                   `json:"description,omitempty"`
	Domains       []Domain                 `json:"domains"`
	ExternalDeps  []string                 `json:"external_deps,omitempty"`
	CriticalFiles []CriticalFile           `json:"critical_files,omitempty"`
	Stats         Stats                    `json:"stats"`
	Cycles        []CircularDependencyCycle `json:"cycles,omitempty"`
	UpdatedAt     time.Time                `json:"updated_at"`
}

// Subdomain represents a named sub-area within a domain.
type Subdomain struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Domain represents a semantic domain within the project.
type Domain struct {
	Name             string      `json:"name"`
	Description      string      `json:"description"`
	KeyFiles         []string    `json:"key_files"`
	Responsibilities []string    `json:"responsibilities"`
	Subdomains       []Subdomain `json:"subdomains,omitempty"`
	DependsOn        []string    `json:"depends_on,omitempty"`
}

// Stats holds codebase statistics.
type Stats struct {
	TotalFiles               int      `json:"total_files"`
	TotalFunctions           int      `json:"total_functions"`
	Languages                []string `json:"languages,omitempty"`
	CircularDependencyCycles int      `json:"circular_dependency_cycles,omitempty"`
}

// CircularDependencyCycle represents a single circular import chain.
type CircularDependencyCycle struct {
	Cycle []string `json:"cycle"`
}

// CircularDependencyResponse is the result from the circular dependency endpoint.
type CircularDependencyResponse struct {
	Cycles []CircularDependencyCycle `json:"cycles"`
}

// JobStatus is the async envelope returned by the Supermodel API.
// Status values: "pending", "processing", "completed", "failed".
type JobStatus struct {
	JobID      string           `json:"jobId"`
	Status     string           `json:"status"`
	RetryAfter int              `json:"retryAfter,omitempty"`
	Result     *json.RawMessage `json:"result,omitempty"`
	Error      string           `json:"error,omitempty"`
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

// buildMultipartBody constructs the multipart/form-data body shared by both graph endpoints.
func buildMultipartBody(projectName string, repoZip []byte) (bodyBytes []byte, contentType string, err error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err = mw.WriteField("project_name", projectName); err != nil {
		return nil, "", fmt.Errorf("writing project_name field: %w", err)
	}
	fw, err := mw.CreateFormFile("file", "repo.zip")
	if err != nil {
		return nil, "", fmt.Errorf("creating multipart field: %w", err)
	}
	if _, err = fw.Write(repoZip); err != nil {
		return nil, "", fmt.Errorf("writing zip: %w", err)
	}
	if err = mw.Close(); err != nil {
		return nil, "", fmt.Errorf("closing multipart: %w", err)
	}
	return buf.Bytes(), mw.FormDataContentType(), nil
}

// pollJob submits a pre-built multipart request to endpoint and polls until the async job
// completes or the context is cancelled. onComplete is called with the raw result payload
// when the job status is "completed"; the payload may be nil if the server returned none.
// notFound, if non-nil, is called when the server returns 404 or 405; returning nil from
// notFound stops polling with no error (caller interprets the absence as "unavailable").
//
// To save upload bandwidth the function uses a two-phase approach:
//  1. Submit phase (attempt 0): POST the full multipart body once to create the job and
//     capture the returned jobId.
//  2. Poll phase (subsequent attempts): GET /v1/jobs/{jobId} — a zero-body request that
//     avoids re-uploading the repo zip on every poll cycle.
//
// If the server responds to the GET with 404 or 405 (status endpoint not available),
// useJobStatusEndpoint is set to false and that probe is not counted against the poll
// budget; subsequent attempts fall back to re-posting the full body with the original
// idempotency key, preserving the existing server-side deduplication behaviour.
func (c *Client) pollJob(
	ctx context.Context,
	endpoint string,
	bodyBytes []byte,
	contentType string,
	idempotencyKey string,
	onComplete func(*json.RawMessage) error,
	notFound func() error,
) error {
	deadline := time.Now().Add(maxPollDuration)

	var jobID string           // captured on the first successful response
	useJobStatusEndpoint := true // try GET /v1/jobs/{jobId} after the initial submit

	for attempt := 0; attempt < maxPollAttempts; attempt++ {
		if time.Now().After(deadline) {
			return fmt.Errorf("job timed out after %v", maxPollDuration)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var (
			req                    *http.Request
			err                    error
			viaJobStatusEndpoint   bool
		)

		if jobID != "" && useJobStatusEndpoint {
			// Lightweight poll: fetch job status without re-uploading the zip.
			viaJobStatusEndpoint = true
			req, err = http.NewRequestWithContext(ctx, http.MethodGet,
				c.baseURL+"/v1/jobs/"+jobID, nil)
			if err != nil {
				return err
			}
			req.Header.Set("X-Api-Key", c.apiKey)
			req.Header.Set("Accept", "application/json")
			req.Header.Set("User-Agent", "uncompact/1.0")
		} else {
			// Full submit: POST with the complete multipart body.
			req, err = http.NewRequestWithContext(ctx, http.MethodPost,
				c.baseURL+endpoint, bytes.NewReader(bodyBytes))
			if err != nil {
				return err
			}
			req.Header.Set("Content-Type", contentType)
			req.Header.Set("X-Api-Key", c.apiKey)
			req.Header.Set("Accept", "application/json")
			req.Header.Set("User-Agent", "uncompact/1.0")
			req.Header.Set("Idempotency-Key", idempotencyKey)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			c.logFn("[warn] poll attempt %d (%s): request error (will retry): %v", attempt+1, endpoint, err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(10 * time.Second):
			}
			continue
		}
		respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
		resp.Body.Close()
		if readErr != nil {
			c.logFn("[warn] poll attempt %d (%s): error reading response (will retry): %v", attempt+1, endpoint, readErr)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(10 * time.Second):
			}
			continue
		}

		c.logFn("[debug] poll attempt %d (%s): HTTP %d", attempt+1, endpoint, resp.StatusCode)

		// If the lightweight GET probe hit an unavailable endpoint, disable it and
		// retry this slot with the full POST body (don't burn a poll attempt).
		if viaJobStatusEndpoint && (resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed) {
			c.logFn("[debug] job status endpoint unavailable; falling back to full POST for polling")
			useJobStatusEndpoint = false
			attempt-- // don't count this probe against the poll budget
			continue
		}

		isOK := false
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			return fmt.Errorf("authentication failed: check your API key at %s", config.DashboardURL)
		case http.StatusPaymentRequired:
			return fmt.Errorf("subscription required: visit %s to subscribe", config.DashboardURL)
		case http.StatusTooManyRequests:
			return fmt.Errorf("rate limit exceeded: please wait before retrying")
		case http.StatusNotFound, http.StatusMethodNotAllowed:
			if notFound != nil {
				return notFound()
			}
		case http.StatusOK, http.StatusAccepted:
			isOK = true
		}
		if !isOK {
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
			return fmt.Errorf("API error %d: %s", resp.StatusCode, msg)
		}

		var jobResp JobStatus
		if err := json.Unmarshal(respBody, &jobResp); err != nil {
			return fmt.Errorf("parsing response: %w", err)
		}

		// Capture the job ID on the first successful response so subsequent poll
		// attempts can use the lightweight GET /v1/jobs/{jobId} endpoint.
		if jobResp.JobID != "" && jobID == "" {
			jobID = jobResp.JobID
			c.logFn("[debug] captured job ID %s; subsequent polls will use GET /v1/jobs/%s", jobID, jobID)
		}

		c.logFn("[debug] job %s status: %s", jobResp.JobID, jobResp.Status)

		switch jobResp.Status {
		case "completed":
			return onComplete(jobResp.Result)
		case "failed":
			return fmt.Errorf("API job failed: %s", jobResp.Error)
		case "pending", "processing":
			retryAfter := time.Duration(jobResp.RetryAfter) * time.Second
			if retryAfter <= 0 {
				retryAfter = 10 * time.Second
			}
			c.logFn("[debug] waiting %v before next poll", retryAfter)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retryAfter):
			}
		default:
			c.logFn("[debug] unknown job status: %s \xe2\x80\x94 retrying in 10s", jobResp.Status)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(10 * time.Second):
			}
		}
	}

	return fmt.Errorf("job did not complete after %d attempts", maxPollAttempts)
}

// GetGraph submits the repo zip and retrieves the project graph, handling async polling.
func (c *Client) GetGraph(ctx context.Context, projectName string, repoZip []byte) (*ProjectGraph, error) {
	c.logFn("[debug] submitting repo to Supermodel API (%d bytes)", len(repoZip))

	bodyBytes, contentType, err := buildMultipartBody(projectName, repoZip)
	if err != nil {
		return nil, err
	}

	var graph *ProjectGraph
	if err := c.pollJob(ctx, "/v1/graphs/supermodel", bodyBytes, contentType, uuid.NewString(),
		func(raw *json.RawMessage) error {
			if raw == nil {
				return fmt.Errorf("job completed but no graph data returned")
			}
			var ir SupermodelIR
			if err := json.Unmarshal(*raw, &ir); err != nil {
				return fmt.Errorf("parsing SupermodelIR result: %w", err)
			}
			graph = ir.toProjectGraph(projectName)
			return nil
		},
		nil,
	); err != nil {
		return nil, err
	}
	return graph, nil
}

// GetCircularDependencies submits the repo zip to the circular dependency endpoint
// and returns the list of detected import cycles. Returns nil, nil if the endpoint
// is unavailable. If available but no cycles are found, returns an empty response.
func (c *Client) GetCircularDependencies(ctx context.Context, projectName string, repoZip []byte) (*CircularDependencyResponse, error) {
	c.logFn("[debug] checking circular dependencies (%d bytes)", len(repoZip))

	bodyBytes, contentType, err := buildMultipartBody(projectName, repoZip)
	if err != nil {
		return nil, err
	}

	var result *CircularDependencyResponse
	if err := c.pollJob(ctx, "/v1/graphs/circular-dependencies", bodyBytes, contentType, uuid.NewString(),
		func(raw *json.RawMessage) error {
			result = &CircularDependencyResponse{}
			if raw == nil {
				return nil
			}
			return json.Unmarshal(*raw, result)
		},
		func() error { return nil }, // 404/405 → endpoint unavailable, return nil, nil
	); err != nil {
		return nil, err
	}
	return result, nil
}

// ValidateKey checks if the API key is valid by probing the graphs endpoint.
// A GET to /v1/graphs/supermodel returns 405 (Method Not Allowed) for valid keys
// and 401/403 for invalid ones.
func (c *Client) ValidateKey(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/v1/graphs/supermodel", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Api-Key", c.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "uncompact/1.0")
	req.Header.Set("Idempotency-Key", uuid.NewString())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("auth check failed: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "", fmt.Errorf("invalid API key")
	case http.StatusMethodNotAllowed, http.StatusOK:
		// Key is valid; /v1/graphs/supermodel only accepts POST
		return "ok", nil
	default:
		return "", fmt.Errorf("auth check failed with status %d", resp.StatusCode)
	}
}
