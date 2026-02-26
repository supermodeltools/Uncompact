package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

const (
	defaultTimeout    = 120 * time.Second
	pollTimeout       = 5 * time.Minute
	defaultRetryAfter = 5 * time.Second
)

// Client is a Supermodel API client.
type Client struct {
	apiKey  string
	baseURL string
	http    *http.Client
}

// New creates a new API client.
func New(apiKey, baseURL string) *Client {
	return &Client{
		apiKey:  apiKey,
		baseURL: baseURL,
		http: &http.Client{
			Timeout: defaultTimeout,
		},
	}
}

// GenerateSupermodelIR uploads a zipped repo and returns the full Supermodel IR.
// It polls until the job completes or the poll timeout is reached.
func (c *Client) GenerateSupermodelIR(repoZip []byte, idempotencyKey string) (*SupermodelIR, error) {
	deadline := time.Now().Add(pollTimeout)
	for {
		result, retryAfter, err := c.postSupermodelGraph(repoZip, idempotencyKey)
		if err != nil {
			return nil, err
		}
		if result != nil {
			return result, nil
		}
		// Job still processing
		if time.Now().Add(retryAfter).After(deadline) {
			return nil, fmt.Errorf("job timed out after %s", pollTimeout)
		}
		time.Sleep(retryAfter)
	}
}

// postSupermodelGraph sends one request. Returns (result, retryAfter, err).
// If result is nil and err is nil, the job is still processing.
func (c *Client) postSupermodelGraph(repoZip []byte, idempotencyKey string) (*SupermodelIR, time.Duration, error) {
	body, contentType, err := buildMultipartBody(repoZip, "repo.zip")
	if err != nil {
		return nil, 0, fmt.Errorf("building request: %w", err)
	}

	req, err := http.NewRequest("POST", c.baseURL+"/v1/graphs/supermodel", body)
	if err != nil {
		return nil, 0, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Api-Key", c.apiKey)
	req.Header.Set("Idempotency-Key", idempotencyKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("reading response: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		var envelope SupermodelIRAsync
		if err := json.Unmarshal(respBytes, &envelope); err != nil {
			return nil, 0, fmt.Errorf("parsing response: %w", err)
		}
		if envelope.Result == nil {
			return nil, 0, fmt.Errorf("API returned 200 but result is empty")
		}
		return envelope.Result, 0, nil

	case http.StatusAccepted:
		var envelope SupermodelIRAsync
		if err := json.Unmarshal(respBytes, &envelope); err != nil {
			return nil, defaultRetryAfter, nil
		}
		retryAfter := defaultRetryAfter
		if envelope.RetryAfter > 0 {
			retryAfter = time.Duration(envelope.RetryAfter) * time.Second
		}
		return nil, retryAfter, nil

	case http.StatusUnauthorized, http.StatusForbidden:
		var apiErr APIError
		_ = json.Unmarshal(respBytes, &apiErr)
		if apiErr.Message != "" {
			return nil, 0, fmt.Errorf("authentication failed: %s — run `uncompact auth login`", apiErr.Message)
		}
		return nil, 0, fmt.Errorf("authentication failed (HTTP %d) — run `uncompact auth login`", resp.StatusCode)

	case http.StatusTooManyRequests:
		return nil, 0, fmt.Errorf("rate limit exceeded — try again later")

	default:
		var apiErr APIError
		_ = json.Unmarshal(respBytes, &apiErr)
		if apiErr.Message != "" {
			return nil, 0, fmt.Errorf("API error (HTTP %d): %s", resp.StatusCode, apiErr.Message)
		}
		return nil, 0, fmt.Errorf("API error (HTTP %d)", resp.StatusCode)
	}
}

// buildMultipartBody creates a multipart/form-data body with the file field.
func buildMultipartBody(data []byte, filename string) (*bytes.Buffer, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(data); err != nil {
		return nil, "", err
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return &buf, w.FormDataContentType(), nil
}
