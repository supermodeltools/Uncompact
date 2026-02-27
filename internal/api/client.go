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
	Name          string         `json:"name"`
	Language      string         `json:"language"`
	Framework     string         `json:"framework,omitempty"`
	Description   string         `json:"description,omitempty"`
	Domains       []Domain       `json:"domains"`
	ExternalDeps  []string       `json:"external_deps,omitempty"`
	CriticalFiles []CriticalFile `json:"critical_files,omitempty"`
	Stats         Stats          `json:"stats"`
	UpdatedAt     time.Time      `json:"updated_at"`
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

// GetGraph submits the repo zip and retrieves the project graph, handling async polling.
// Polling is done by re-submitting the same POST with the same idempotency key; the
// server returns cached job status on subsequent calls with the same key.
func (c *Client) GetGraph(ctx context.Context, projectName string, repoZip []byte) (*ProjectGraph, error) {
	c.logFn("[debug] submitting repo to Supermodel API (%d bytes)", len(repoZip))

	idempotencyKey := uuid.NewString()
	deadline := time.Now().Add(maxPollDuration)

	for attempt := 0; attempt < maxPollAttempts; attempt++ {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("job timed out after %v", maxPollDuration)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Build multipart body on each attempt (re-POST with same idempotency key)
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		_ = mw.WriteField("project_name", projectName)
		fw, err := mw.CreateFormFile("file", "repo.zip")
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
		req.Header.Set("X-Api-Key", c.apiKey)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "uncompact/1.0")
		req.Header.Set("Idempotency-Key", idempotencyKey)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			c.logFn("[warn] poll attempt %d: request error (will retry): %v", attempt+1, err)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(10 * time.Second):
			}
			continue
		}
		respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
		resp.Body.Close()
		if readErr != nil {
			c.logFn("[warn] poll attempt %d: error reading response (will retry): %v", attempt+1, readErr)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(10 * time.Second):
			}
			continue
		}

		c.logFn("[debug] poll attempt %d: HTTP %d", attempt+1, resp.StatusCode)

		switch resp.StatusCode {
		case http.StatusUnauthorized:
			return nil, fmt.Errorf("authentication failed: check your API key at %s", config.DashboardURL)
		case http.StatusPaymentRequired:
			return nil, fmt.Errorf("subscription required: visit %s to subscribe", config.DashboardURL)
		case http.StatusTooManyRequests:
			return nil, fmt.Errorf("rate limit exceeded: please wait before retrying")
		case http.StatusOK, http.StatusAccepted:
			// Both 200 and 202 return the same async envelope
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

		var jobResp JobStatus
		if err := json.Unmarshal(respBody, &jobResp); err != nil {
			return nil, fmt.Errorf("parsing response: %w", err)
		}

		c.logFn("[debug] job %s status: %s", jobResp.JobID, jobResp.Status)

		switch jobResp.Status {
		case "completed":
			if jobResp.Result == nil {
				return nil, fmt.Errorf("job completed but no graph data returned")
			}
			var ir SupermodelIR
			if err := json.Unmarshal(*jobResp.Result, &ir); err != nil {
				return nil, fmt.Errorf("parsing SupermodelIR result: %w", err)
			}
			return ir.toProjectGraph(projectName), nil
		case "failed":
			return nil, fmt.Errorf("API job failed: %s", jobResp.Error)
		case "pending", "processing":
			retryAfter := time.Duration(jobResp.RetryAfter) * time.Second
			if retryAfter <= 0 {
				retryAfter = 10 * time.Second
			}
			c.logFn("[debug] waiting %v before next poll", retryAfter)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(retryAfter):
			}
		default:
			c.logFn("[debug] unknown job status: %s", jobResp.Status)
		}
	}

	return nil, fmt.Errorf("job did not complete after %d attempts", maxPollAttempts)
}

// GetCircularDependencies submits the repo zip to the circular dependency endpoint
// and returns the list of detected import cycles. Returns nil, nil if the endpoint
// is unavailable. If available but no cycles are found, returns an empty response.
func (c *Client) GetCircularDependencies(ctx context.Context, projectName string, repoZip []byte) (*CircularDependencyResponse, error) {
	c.logFn("[debug] checking circular dependencies (%d bytes)", len(repoZip))

	idempotencyKey := uuid.NewString()
	deadline := time.Now().Add(maxPollDuration)

	for attempt := 0; attempt < maxPollAttempts; attempt++ {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("circular dependency job timed out after %v", maxPollDuration)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		_ = mw.WriteField("project_name", projectName)
		fw, err := mw.CreateFormFile("file", "repo.zip")
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
			c.baseURL+"/v1/graphs/circular-dependencies", &body)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("X-Api-Key", c.apiKey)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "uncompact/1.0")
		req.Header.Set("Idempotency-Key", idempotencyKey)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			c.logFn("[warn] circular dep poll attempt %d: request error (will retry): %v", attempt+1, err)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(10 * time.Second):
			}
			continue
		}
		respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
		resp.Body.Close()
		if readErr != nil {
			c.logFn("[warn] circular dep poll attempt %d: error reading response (will retry): %v", attempt+1, readErr)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(10 * time.Second):
			}
			continue
		}

		c.logFn("[debug] circular dep poll attempt %d: HTTP %d", attempt+1, resp.StatusCode)

		switch resp.StatusCode {
		case http.StatusUnauthorized:
			return nil, fmt.Errorf("authentication failed: check your API key at %s", config.DashboardURL)
		case http.StatusPaymentRequired:
			return nil, fmt.Errorf("subscription required: visit %s to subscribe", config.DashboardURL)
		case http.StatusTooManyRequests:
			return nil, fmt.Errorf("rate limit exceeded: please wait before retrying")
		case http.StatusNotFound, http.StatusMethodNotAllowed:
			// Endpoint not available — treat as no data
			return nil, nil
		case http.StatusOK, http.StatusAccepted:
			// Continue to parse
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

		var jobResp JobStatus
		if err := json.Unmarshal(respBody, &jobResp); err != nil {
			return nil, fmt.Errorf("parsing response: %w", err)
		}

		c.logFn("[debug] circular dep job %s status: %s", jobResp.JobID, jobResp.Status)

		switch jobResp.Status {
		case "completed":
			if jobResp.Result == nil {
				return &CircularDependencyResponse{}, nil
			}
			var result CircularDependencyResponse
			if err := json.Unmarshal(*jobResp.Result, &result); err != nil {
				return nil, fmt.Errorf("parsing circular dependency result: %w", err)
			}
			return &result, nil
		case "failed":
			return nil, fmt.Errorf("circular dependency job failed: %s", jobResp.Error)
		case "pending", "processing":
			retryAfter := time.Duration(jobResp.RetryAfter) * time.Second
			if retryAfter <= 0 {
				retryAfter = 10 * time.Second
			}
			c.logFn("[debug] waiting %v before next circular dep poll", retryAfter)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(retryAfter):
			}
		default:
			c.logFn("[debug] unknown circular dep job status: %s", jobResp.Status)
		}
	}

	return nil, fmt.Errorf("circular dependency job did not complete after %d attempts", maxPollAttempts)
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
