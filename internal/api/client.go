package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/supermodeltools/uncompact/internal/db"
)

const (
	baseURL        = "https://api.supermodeltools.com"
	defaultTimeout = 10 * time.Second
	authEnvVar     = "SUPERMODEL_API_KEY"
	dashboardURL   = "https://dashboard.supermodeltools.com"
)

// Client is the Supermodel API client.
type Client struct {
	httpClient *http.Client
	apiKey     string
	baseURL    string
}

// Graph represents the context graph returned by the Supermodel API.
type Graph struct {
	ProjectID string         `json:"project_id"`
	FetchedAt time.Time      `json:"fetched_at"`
	TTL       time.Duration  `json:"ttl"`
	Source    string         `json:"source"`
	Sections  []GraphSection `json:"sections"`
}

// GraphSection is a prioritized section of context within a Graph.
type GraphSection struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Content    string  `json:"content"`
	Priority   string  `json:"priority"` // "required" or "optional"
	Confidence float64 `json:"confidence"`
	Tokens     int     `json:"tokens"`
	Source     string  `json:"source"`
}

// NewClient creates a new Supermodel API client.
// The API key is read from SUPERMODEL_API_KEY, falling back to ~/.uncompact/config.
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: defaultTimeout},
		apiKey:     loadAPIKey(),
		baseURL:    baseURL,
	}
}

// FetchGraph retrieves the context graph from the API or the local cache.
func (c *Client) FetchGraph(ctx context.Context, forceRefresh bool, store *db.Store) (*Graph, error) {
	if !forceRefresh {
		cached, err := store.GetLatest()
		if err == nil && cached != nil {
			age := time.Since(cached.FetchedAt)
			if age < cached.TTL {
				return graphFromRecord(cached)
			}
		}
	}

	if c.apiKey == "" {
		return nil, fmt.Errorf("no API key found; subscribe and get your key at %s", dashboardURL)
	}

	graph, err := c.fetchFromAPI(ctx)
	if err != nil {
		return nil, err
	}

	if storeErr := c.cacheGraph(store, graph); storeErr != nil {
		fmt.Fprintf(os.Stderr, "warning: could not cache graph: %v\n", storeErr)
	}

	return graph, nil
}

func (c *Client) fetchFromAPI(ctx context.Context) (*Graph, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/graph", nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "uncompact/0.1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return nil, fmt.Errorf("invalid API key; manage your subscription at %s", dashboardURL)
	case http.StatusPaymentRequired:
		return nil, fmt.Errorf("subscription required; subscribe at %s", dashboardURL)
	case http.StatusOK:
		// continue
	default:
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var graph Graph
	if err := json.NewDecoder(resp.Body).Decode(&graph); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	graph.FetchedAt = time.Now()
	if graph.TTL == 0 {
		graph.TTL = time.Hour
	}
	graph.Source = "api"

	return &graph, nil
}

func (c *Client) cacheGraph(store *db.Store, graph *Graph) error {
	data, err := json.Marshal(graph)
	if err != nil {
		return err
	}

	tokens := 0
	for _, s := range graph.Sections {
		tokens += s.Tokens
	}

	record := &db.GraphRecord{
		ProjectID:       graph.ProjectID,
		FetchedAt:       graph.FetchedAt,
		TTL:             graph.TTL,
		Source:          graph.Source,
		EstimatedTokens: tokens,
		Data:            data,
	}
	return store.SaveRecord(record)
}

// GraphFromRecord deserializes a cached GraphRecord back into a Graph.
func GraphFromRecord(r *db.GraphRecord) (*Graph, error) {
	var g Graph
	if err := json.Unmarshal(r.Data, &g); err != nil {
		return nil, fmt.Errorf("deserializing cached graph: %w", err)
	}
	g.FetchedAt = r.FetchedAt
	g.TTL = r.TTL
	g.Source = "cache"
	return &g, nil
}

func loadAPIKey() string {
	if key := os.Getenv(authEnvVar); key != "" {
		return key
	}
	// TODO: read from ~/.uncompact/config
	return ""
}
