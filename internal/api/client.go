package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/supermodeltools/uncompact/internal/config"
)

// Client is the Supermodel API client.
type Client struct {
	cfg        *config.Config
	httpClient *http.Client
}

// NewClient creates a new API client.
func NewClient(cfg *config.Config) *Client {
	return &Client{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// WorkspaceContext describes the current workspace for the API.
type WorkspaceContext struct {
	WorkspacePath string `json:"workspace_path"`
	WorkspaceName string `json:"workspace_name"`
}

// GraphOutput is the response from the Supermodel API graph endpoint.
// NOTE: Update field names/types when the OpenAPI spec is confirmed.
type GraphOutput struct {
	FetchedAt   time.Time         `json:"fetched_at"`
	ExpiresAt   time.Time         `json:"expires_at"`
	WorkspaceID string            `json:"workspace_id,omitempty"`
	Overview    string            `json:"overview,omitempty"`
	Nodes       []GraphNode       `json:"nodes,omitempty"`
	Edges       []GraphEdge       `json:"edges,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	RawJSON     json.RawMessage   `json:"raw,omitempty"`
}

// IsExpired returns true if the cached data is past its TTL.
func (g *GraphOutput) IsExpired() bool {
	return !g.ExpiresAt.IsZero() && time.Now().After(g.ExpiresAt)
}

// GraphNode represents an entity in the context graph.
type GraphNode struct {
	ID       string            `json:"id"`
	Type     string            `json:"type"`
	Label    string            `json:"label"`
	Content  string            `json:"content,omitempty"`
	Priority string            `json:"priority,omitempty"` // "required" | "optional"
	Tags     []string          `json:"tags,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// GraphEdge represents a relationship in the context graph.
type GraphEdge struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Relation string `json:"relation"`
}

// AccountInfo holds subscription information from the API.
type AccountInfo struct {
	Email             string `json:"email"`
	SubscriptionTier  string `json:"subscription_tier"`
	APICallsRemaining int    `json:"api_calls_remaining"`
}

// ValidateAuth checks if the current API key is valid.
func (c *Client) ValidateAuth(ctx context.Context) error {
	_, err := c.GetAccountInfo(ctx)
	return err
}

// GetAccountInfo fetches account info for the authenticated user.
func (c *Client) GetAccountInfo(ctx context.Context) (*AccountInfo, error) {
	var info AccountInfo
	if err := c.get(ctx, "/v1/account", &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// GetContextGraph fetches the context graph for a workspace.
// NOTE: Endpoint path and request shape will be updated when the OpenAPI spec is confirmed.
func (c *Client) GetContextGraph(ctx context.Context, ws *WorkspaceContext) (*GraphOutput, error) {
	body, err := json.Marshal(ws)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	var graph GraphOutput
	if err := c.post(ctx, "/v1/graph", body, &graph); err != nil {
		return nil, err
	}

	graph.FetchedAt = time.Now()
	return &graph, nil
}

func (c *Client) get(ctx context.Context, path string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url(path), nil)
	if err != nil {
		return err
	}
	return c.do(req, out)
}

func (c *Client) post(ctx context.Context, path string, body []byte, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url(path), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, out)
}

func (c *Client) do(req *http.Request, out interface{}) error {
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	req.Header.Set("User-Agent", "uncompact/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("authentication failed (401): check your API key at https://dashboard.supermodeltools.com")
	}
	if resp.StatusCode == http.StatusPaymentRequired {
		return fmt.Errorf("subscription required (402): subscribe at https://dashboard.supermodeltools.com")
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("rate limit exceeded (429): try again later")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("API error %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}

	return nil
}

func (c *Client) url(path string) string {
	base := c.cfg.APIURL
	if base == "" {
		base = "https://api.supermodeltools.com"
	}
	return base + path
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
