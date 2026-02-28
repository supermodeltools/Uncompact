package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// instantAfter returns a channel that fires immediately, replacing time.After in
// tests so that poll backoffs complete without real sleeping.
func instantAfter(_ time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	ch <- time.Now()
	return ch
}

// newTestClient builds a Client pointing at srv.
// If after is non-nil it is installed so tests can control poll delays.
func newTestClient(t *testing.T, srv *httptest.Server, after func(time.Duration) <-chan time.Time) *Client {
	t.Helper()
	c := New(srv.URL, "test-key", false, nil)
	if after != nil {
		c.afterFn = after
	}
	return c
}

// pendingBody returns a JSON-encoded pending JobStatus with the given job ID.
func pendingBody(jobID string) []byte {
	b, _ := json.Marshal(JobStatus{JobID: jobID, Status: "pending"})
	return b
}

// completedBody returns a JSON-encoded completed JobStatus wrapping resultJSON.
func completedBody(jobID string, resultJSON []byte) []byte {
	raw := json.RawMessage(resultJSON)
	b, _ := json.Marshal(JobStatus{JobID: jobID, Status: "completed", Result: &raw})
	return b
}

// minSupermodelIR is the smallest valid SupermodelIR payload for test responses.
const minSupermodelIR = `{"repo":"test","summary":{"primaryLanguage":"go"},"metadata":{"languages":["go"],"fileCount":1},"domains":[],"graph":{"nodes":[],"relationships":[]}}`

// ——— 1. Poll loop exhaustion ——————————————————————————————————————————————————

func TestPollJob_ExhaustsMaxAttempts(t *testing.T) {
	// Server always returns "pending"; the client must exhaust maxPollAttempts
	// and return the well-known error string.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(pendingBody("job-1"))
	}))
	defer ts.Close()

	c := newTestClient(t, ts, instantAfter)
	_, err := c.GetGraph(context.Background(), "proj", []byte("zip"))
	if err == nil {
		t.Fatal("expected error from exhausted poll budget, got nil")
	}
	want := fmt.Sprintf("job did not complete after %d attempts", maxPollAttempts)
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

// ——— 2. Rate-limit back-off honours Retry-After header ————————————————————

func TestPollJob_RateLimitHonoursRetryAfterHeader(t *testing.T) {
	// First request returns 429 with Retry-After: 7. Second returns completed.
	// afterFn records the duration so we can assert it equals 7 s, not the 30 s default.
	var callCount int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&callCount, 1) == 1 {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(completedBody("job-1", []byte(minSupermodelIR)))
	}))
	defer ts.Close()

	var recordedDuration time.Duration
	afterFn := func(d time.Duration) <-chan time.Time {
		if recordedDuration == 0 {
			recordedDuration = d
		}
		ch := make(chan time.Time, 1)
		ch <- time.Now()
		return ch
	}

	c := newTestClient(t, ts, afterFn)
	if _, err := c.GetGraph(context.Background(), "proj", []byte("zip")); err != nil {
		t.Fatalf("GetGraph: %v", err)
	}
	if recordedDuration != 7*time.Second {
		t.Errorf("rate-limit backoff = %v, want 7s", recordedDuration)
	}
}

func TestPollJob_RateLimitDefaultsTo30s_WhenNoRetryAfterHeader(t *testing.T) {
	// 429 with no Retry-After → must fall back to 30 s.
	var callCount int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&callCount, 1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests) // no Retry-After header
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(completedBody("job-1", []byte(minSupermodelIR)))
	}))
	defer ts.Close()

	var recordedDuration time.Duration
	afterFn := func(d time.Duration) <-chan time.Time {
		if recordedDuration == 0 {
			recordedDuration = d
		}
		ch := make(chan time.Time, 1)
		ch <- time.Now()
		return ch
	}

	c := newTestClient(t, ts, afterFn)
	if _, err := c.GetGraph(context.Background(), "proj", []byte("zip")); err != nil {
		t.Fatalf("GetGraph: %v", err)
	}
	if recordedDuration != 30*time.Second {
		t.Errorf("default rate-limit backoff = %v, want 30s", recordedDuration)
	}
}

// ——— 3. Job-status endpoint fallback (404/405 → attempt--) ———————————————

func TestPollJob_JobStatusFallback_DoesNotCountAgainstBudget(t *testing.T) {
	// Expected request sequence:
	//   attempt 0 → POST /v1/graphs/supermodel   → pending  (jobID captured)
	//   attempt 1 → GET  /v1/jobs/job-id         → 404      (probe; attempt--)
	//   attempt 1 → POST /v1/graphs/supermodel   → completed
	// Total requests = 3, but only 2 poll budget slots consumed.
	var (
		totalRequests int32
		postCount     int32
	)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&totalRequests, 1)
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		n := int(atomic.AddInt32(&postCount, 1))
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			w.Write(pendingBody("job-id")) // first POST: pending, captures jobID
		} else {
			w.Write(completedBody("job-id", []byte(minSupermodelIR))) // second POST: done
		}
	}))
	defer ts.Close()

	c := newTestClient(t, ts, instantAfter)
	if _, err := c.GetGraph(context.Background(), "proj", []byte("zip")); err != nil {
		t.Fatalf("GetGraph: %v", err)
	}
	if got := atomic.LoadInt32(&totalRequests); got != 3 {
		t.Errorf("total HTTP requests = %d, want 3 (POST pending, GET 404 probe, POST completed)", got)
	}
	if got := atomic.LoadInt32(&postCount); got != 2 {
		t.Errorf("POST requests = %d, want 2", got)
	}
}

// ——— 4. Context cancellation mid-poll ————————————————————————————————————

func TestPollJob_ContextCancellation_ReturnsCancelled(t *testing.T) {
	// Server always returns pending. afterFn cancels the context on its first
	// call (simulating cancellation during a backoff wait), then returns a
	// channel that never fires; ctx.Done() should win in the select.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(pendingBody("job-1"))
	}))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var triggered int32
	afterFn := func(d time.Duration) <-chan time.Time {
		if atomic.CompareAndSwapInt32(&triggered, 0, 1) {
			cancel()                    // mark context done before returning
			return make(chan time.Time)  // never fires; ctx.Done() wins
		}
		ch := make(chan time.Time, 1)
		ch <- time.Now()
		return ch
	}

	c := newTestClient(t, ts, afterFn)
	_, err := c.GetGraph(ctx, "proj", []byte("zip"))
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if err != context.Canceled {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

// ——— 5. GetGraphAndCircularDeps concurrency ——————————————————————————————

func TestGetGraphAndCircularDeps_BothSucceed(t *testing.T) {
	// Both goroutines should send their requests; verify both endpoints are hit
	// and the combined result is populated correctly.
	var (
		graphHits int32
		circHits  int32
	)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/graphs/supermodel":
			atomic.AddInt32(&graphHits, 1)
			w.Write(completedBody("job-g", []byte(minSupermodelIR)))
		case "/v1/graphs/circular-dependencies":
			atomic.AddInt32(&circHits, 1)
			cycles, _ := json.Marshal(CircularDependencyResponse{
				Cycles: []CircularDependencyCycle{{Cycle: []string{"a.go", "b.go"}}},
			})
			w.Write(completedBody("job-c", cycles))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	c := newTestClient(t, ts, instantAfter)
	graph, err := c.GetGraphAndCircularDeps(context.Background(), "proj", []byte("zip"))
	if err != nil {
		t.Fatalf("GetGraphAndCircularDeps: %v", err)
	}
	if graph == nil {
		t.Fatal("expected non-nil graph")
	}
	if !graph.CircularDepsAnalyzed {
		t.Error("CircularDepsAnalyzed should be true")
	}
	if graph.Stats.CircularDependencyCycles != 1 {
		t.Errorf("CircularDependencyCycles = %d, want 1", graph.Stats.CircularDependencyCycles)
	}
	if atomic.LoadInt32(&graphHits) == 0 {
		t.Error("graph endpoint was never called")
	}
	if atomic.LoadInt32(&circHits) == 0 {
		t.Error("circular-dependencies endpoint was never called")
	}
}

func TestGetGraphAndCircularDeps_GraphErrorReturnsError(t *testing.T) {
	// When the graph job fails, GetGraphAndCircularDeps must return that error.
	// The circ-dep goroutine uses a buffered channel (cap 1) and terminates
	// cleanly even after the context is cancelled by the deferred cancel().
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/graphs/supermodel":
			b, _ := json.Marshal(JobStatus{Status: "failed", Error: "upstream error"})
			w.Write(b)
		case "/v1/graphs/circular-dependencies":
			raw := json.RawMessage(`{"cycles":[]}`)
			b, _ := json.Marshal(JobStatus{Status: "completed", Result: &raw})
			w.Write(b)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	c := newTestClient(t, ts, instantAfter)
	_, err := c.GetGraphAndCircularDeps(context.Background(), "proj", []byte("zip"))
	if err == nil {
		t.Fatal("expected error when graph job fails, got nil")
	}
	if !strings.Contains(err.Error(), "upstream error") {
		t.Errorf("error = %q, want to contain \"upstream error\"", err.Error())
	}
}

func TestGetGraphAndCircularDeps_CircDepUnavailable(t *testing.T) {
	// When the circular-deps endpoint returns 404, GetGraphAndCircularDeps should
	// still return a valid graph. CircularDepsAnalyzed is set to true (nil circDeps
	// means the endpoint responded but had no cycles to report via notFound handler).
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/graphs/supermodel":
			w.Write(completedBody("job-g", []byte(minSupermodelIR)))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	c := newTestClient(t, ts, instantAfter)
	graph, err := c.GetGraphAndCircularDeps(context.Background(), "proj", []byte("zip"))
	if err != nil {
		t.Fatalf("GetGraphAndCircularDeps: %v", err)
	}
	if graph == nil {
		t.Fatal("expected non-nil graph")
	}
	if graph.Stats.CircularDependencyCycles != 0 {
		t.Errorf("CircularDependencyCycles = %d, want 0", graph.Stats.CircularDependencyCycles)
	}
}

// ——— 6. Error response parsing ———————————————————————————————————————————

func TestPollJob_ErrorParsing_JSONMessageField(t *testing.T) {
	// A non-2xx response with a JSON body containing a "message" field should
	// produce an error string that includes that message.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"message":"quota exceeded","error":""}`))
	}))
	defer ts.Close()

	c := newTestClient(t, ts, instantAfter)
	_, err := c.GetGraph(context.Background(), "proj", []byte("zip"))
	if err == nil {
		t.Fatal("expected error for 400 response, got nil")
	}
	if !strings.Contains(err.Error(), "quota exceeded") {
		t.Errorf("error = %q, want to contain \"quota exceeded\"", err.Error())
	}
}

func TestPollJob_ErrorParsing_JSONErrorField(t *testing.T) {
	// When "message" is absent but "error" is present, the "error" field value
	// should appear in the returned error string.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid repo format"}`))
	}))
	defer ts.Close()

	c := newTestClient(t, ts, instantAfter)
	_, err := c.GetGraph(context.Background(), "proj", []byte("zip"))
	if err == nil {
		t.Fatal("expected error for 400 response, got nil")
	}
	if !strings.Contains(err.Error(), "invalid repo format") {
		t.Errorf("error = %q, want to contain \"invalid repo format\"", err.Error())
	}
}

func TestPollJob_ServerError_Retries(t *testing.T) {
	// 5xx responses are retried; after some transient errors the client should
	// eventually succeed when the server recovers.
	var callCount int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&callCount, 1) <= 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(completedBody("job-1", []byte(minSupermodelIR)))
	}))
	defer ts.Close()

	c := newTestClient(t, ts, instantAfter)
	if _, err := c.GetGraph(context.Background(), "proj", []byte("zip")); err != nil {
		t.Fatalf("GetGraph after transient 5xx errors: %v", err)
	}
	if got := atomic.LoadInt32(&callCount); got != 4 {
		t.Errorf("callCount = %d, want 4 (3 errors + 1 success)", got)
	}
}

func TestPollJob_ErrorParsing_NonJSON_NonRetriableStatus(t *testing.T) {
	// 400-level (non-429, non-401, non-402, non-404) with a plain-text body:
	// the raw body should appear verbatim in the error.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict) // 409
		w.Write([]byte("job already exists"))
	}))
	defer ts.Close()

	c := newTestClient(t, ts, instantAfter)
	_, err := c.GetGraph(context.Background(), "proj", []byte("zip"))
	if err == nil {
		t.Fatal("expected error for 409 response, got nil")
	}
	if !strings.Contains(err.Error(), "job already exists") {
		t.Errorf("error = %q, want to contain \"job already exists\"", err.Error())
	}
}
