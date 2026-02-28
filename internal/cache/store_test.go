package cache

import (
	"testing"
	"time"

	"github.com/supermodeltools/uncompact/internal/api"
)

// openTestStore opens an in-memory SQLite store and registers cleanup.
func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// sampleGraph returns a minimal ProjectGraph for use in tests.
func sampleGraph() *api.ProjectGraph {
	return &api.ProjectGraph{
		Name:      "test-project",
		Language:  "go",
		UpdatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func TestOpen_CreatesSchema(t *testing.T) {
	s := openTestStore(t)

	var ver int
	if err := s.db.QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&ver); err != nil {
		t.Fatalf("querying schema_version: %v", err)
	}
	if ver != schemaVersion {
		t.Errorf("schema_version = %d, want %d", ver, schemaVersion)
	}
}

func TestOpen_IdempotentMigration(t *testing.T) {
	// Opening the same path twice should not fail (IF NOT EXISTS guards).
	s := openTestStore(t)
	if err := s.migrate(); err != nil {
		t.Fatalf("second migrate call: %v", err)
	}
}

func TestGet_Missing(t *testing.T) {
	s := openTestStore(t)

	graph, found, expiresAt, fetchedAt, err := s.Get("nonexistent-hash")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if found {
		t.Error("expected found=false for missing key")
	}
	if graph != nil {
		t.Error("expected nil graph for missing key")
	}
	if expiresAt != nil || fetchedAt != nil {
		t.Error("expected nil time pointers for missing key")
	}
}

func TestSet_Get_RoundTrip(t *testing.T) {
	s := openTestStore(t)

	want := sampleGraph()
	if err := s.Set("hash-1", "project-1", want); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, isFresh, expiresAt, fetchedAt, err := s.Get("hash-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil graph")
	}
	if !isFresh {
		t.Error("expected isFresh=true for freshly-set entry")
	}
	if got.Name != want.Name {
		t.Errorf("graph.Name = %q, want %q", got.Name, want.Name)
	}
	if got.Language != want.Language {
		t.Errorf("graph.Language = %q, want %q", got.Language, want.Language)
	}
	if expiresAt == nil {
		t.Error("expected non-nil expiresAt")
	}
	if fetchedAt == nil {
		t.Error("expected non-nil fetchedAt")
	}
}

func TestSet_UpdateExisting(t *testing.T) {
	s := openTestStore(t)

	if err := s.Set("hash-1", "project-1", sampleGraph()); err != nil {
		t.Fatalf("initial Set: %v", err)
	}

	updated := &api.ProjectGraph{Name: "updated-project", Language: "typescript"}
	if err := s.Set("hash-1", "project-1", updated); err != nil {
		t.Fatalf("second Set: %v", err)
	}

	got, _, _, _, err := s.Get("hash-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != updated.Name {
		t.Errorf("graph.Name = %q, want %q", got.Name, updated.Name)
	}
	if got.Language != updated.Language {
		t.Errorf("graph.Language = %q, want %q", got.Language, updated.Language)
	}
}

func TestGet_Stale(t *testing.T) {
	s := openTestStore(t)
	// Use a negative TTL so the entry is immediately expired.
	s.ttl = -1 * time.Minute

	if err := s.Set("hash-stale", "project-stale", sampleGraph()); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, isFresh, _, _, err := s.Get("hash-stale")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil graph for stale entry")
	}
	if isFresh {
		t.Error("expected isFresh=false for expired entry")
	}
}

func TestPrune_RemovesOldEntries(t *testing.T) {
	s := openTestStore(t)

	// Insert an entry fetched 60 days ago (older than defaultMaxAge of 30 days).
	oldFetchedAt := time.Now().UTC().Add(-60 * 24 * time.Hour)
	_, err := s.db.Exec(`
		INSERT INTO graph_cache (project_hash, project_name, graph_json, fetched_at, expires_at)
		VALUES (?, ?, ?, ?, ?)`,
		"hash-old", "old-project", `{"name":"old"}`, oldFetchedAt, oldFetchedAt.Add(defaultTTL),
	)
	if err != nil {
		t.Fatalf("inserting old entry: %v", err)
	}

	// Insert a fresh entry.
	if err := s.Set("hash-new", "new-project", sampleGraph()); err != nil {
		t.Fatalf("Set fresh entry: %v", err)
	}

	if err := s.Prune(); err != nil {
		t.Fatalf("Prune: %v", err)
	}

	_, foundOld, _, _, err := s.Get("hash-old")
	if err != nil {
		t.Fatalf("Get old: %v", err)
	}
	if foundOld {
		t.Error("Prune did not remove old entry")
	}

	_, foundNew, _, _, err := s.Get("hash-new")
	if err != nil {
		t.Fatalf("Get new: %v", err)
	}
	if !foundNew {
		t.Error("Prune removed new entry that should remain")
	}
}

func TestPrune_RemovesOldInjectionLogs(t *testing.T) {
	s := openTestStore(t)

	// Insert a log entry created 100 days ago (older than 90-day cutoff).
	oldCreatedAt := time.Now().UTC().Add(-100 * 24 * time.Hour)
	_, err := s.db.Exec(`
		INSERT INTO injection_log (project_hash, project_name, tokens, source, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		"hash-old", "old-project", 100, "api", oldCreatedAt,
	)
	if err != nil {
		t.Fatalf("inserting old log: %v", err)
	}

	if err := s.Prune(); err != nil {
		t.Fatalf("Prune: %v", err)
	}

	logs, err := s.RecentLogs(100)
	if err != nil {
		t.Fatalf("RecentLogs: %v", err)
	}
	if len(logs) != 0 {
		t.Errorf("expected 0 logs after prune, got %d", len(logs))
	}
}

func TestLogInjection_RecentLogs(t *testing.T) {
	s := openTestStore(t)

	staleAt := time.Now().UTC().Add(5 * time.Minute)

	// Insert with explicit timestamps so ordering is deterministic.
	t1 := time.Now().UTC().Add(-2 * time.Minute)
	t2 := time.Now().UTC().Add(-1 * time.Minute)

	_, err := s.db.Exec(`
		INSERT INTO injection_log (project_hash, project_name, tokens, source, stale_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		"hash-1", "project-1", 1234, "api", staleAt, t1,
	)
	if err != nil {
		t.Fatalf("insert first log: %v", err)
	}
	_, err = s.db.Exec(`
		INSERT INTO injection_log (project_hash, project_name, tokens, source, stale_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		"hash-1", "project-1", 500, "cache", nil, t2,
	)
	if err != nil {
		t.Fatalf("insert second log: %v", err)
	}

	logs, err := s.RecentLogs(10)
	if err != nil {
		t.Fatalf("RecentLogs: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("RecentLogs returned %d entries, want 2", len(logs))
	}

	// Most recent first (t2 > t1).
	if logs[0].Tokens != 500 {
		t.Errorf("logs[0].Tokens = %d, want 500 (most recent)", logs[0].Tokens)
	}
	if logs[0].Source != "cache" {
		t.Errorf("logs[0].Source = %q, want cache", logs[0].Source)
	}
	if logs[0].StaleAt != nil {
		t.Error("expected logs[0].StaleAt to be nil")
	}

	if logs[1].Tokens != 1234 {
		t.Errorf("logs[1].Tokens = %d, want 1234 (older)", logs[1].Tokens)
	}
	if logs[1].StaleAt == nil {
		t.Error("expected logs[1].StaleAt to be non-nil")
	}
}

func TestLogInjection_PublicAPI(t *testing.T) {
	s := openTestStore(t)

	staleAt := time.Now().Add(5 * time.Minute)
	if err := s.LogInjection("hash-1", "project-1", 1234, "api", &staleAt); err != nil {
		t.Fatalf("LogInjection with staleAt: %v", err)
	}
	if err := s.LogInjection("hash-2", "project-2", 500, "cache", nil); err != nil {
		t.Fatalf("LogInjection without staleAt: %v", err)
	}

	logs, err := s.RecentLogs(10)
	if err != nil {
		t.Fatalf("RecentLogs: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("got %d logs, want 2", len(logs))
	}
}

func TestRecentLogs_DefaultLimit(t *testing.T) {
	s := openTestStore(t)

	for i := 0; i < 25; i++ {
		if err := s.LogInjection("hash", "project", 100, "api", nil); err != nil {
			t.Fatalf("LogInjection %d: %v", i, err)
		}
	}

	// limit=0 should default to 20.
	logs, err := s.RecentLogs(0)
	if err != nil {
		t.Fatalf("RecentLogs(0): %v", err)
	}
	if len(logs) != 20 {
		t.Errorf("RecentLogs(0) returned %d entries, want 20", len(logs))
	}

	// Explicit limit should be respected.
	logs5, err := s.RecentLogs(5)
	if err != nil {
		t.Fatalf("RecentLogs(5): %v", err)
	}
	if len(logs5) != 5 {
		t.Errorf("RecentLogs(5) returned %d entries, want 5", len(logs5))
	}
}

func TestGetStats(t *testing.T) {
	s := openTestStore(t)

	entries := []struct {
		hash   string
		tokens int
		source string
	}{
		{"hash-1", 1000, "api"},
		{"hash-1", 2000, "api"},
		{"hash-1", 500, "cache"},
		{"hash-1", 300, "stale_cache"},
	}
	for _, e := range entries {
		if err := s.LogInjection(e.hash, "project-1", e.tokens, e.source, nil); err != nil {
			t.Fatalf("LogInjection: %v", err)
		}
	}

	stats, err := s.GetStats("")
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.TotalInjections != 4 {
		t.Errorf("TotalInjections = %d, want 4", stats.TotalInjections)
	}
	if stats.APIFetches != 2 {
		t.Errorf("APIFetches = %d, want 2", stats.APIFetches)
	}
	if stats.CacheHits != 2 { // cache + stale_cache
		t.Errorf("CacheHits = %d, want 2", stats.CacheHits)
	}
	if stats.StaleCacheHits != 1 {
		t.Errorf("StaleCacheHits = %d, want 1", stats.StaleCacheHits)
	}
	if stats.TotalTokens != 3800 {
		t.Errorf("TotalTokens = %d, want 3800", stats.TotalTokens)
	}
	wantAvg := float64(3800) / 4.0
	if stats.AvgTokens != wantAvg {
		t.Errorf("AvgTokens = %v, want %v", stats.AvgTokens, wantAvg)
	}
}

func TestGetStats_FilterByProject(t *testing.T) {
	s := openTestStore(t)

	if err := s.LogInjection("hash-1", "project-1", 1000, "api", nil); err != nil {
		t.Fatalf("LogInjection hash-1: %v", err)
	}
	if err := s.LogInjection("hash-2", "project-2", 2000, "api", nil); err != nil {
		t.Fatalf("LogInjection hash-2: %v", err)
	}

	stats, err := s.GetStats("hash-1")
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.TotalInjections != 1 {
		t.Errorf("TotalInjections = %d, want 1", stats.TotalInjections)
	}
	if stats.TotalTokens != 1000 {
		t.Errorf("TotalTokens = %d, want 1000", stats.TotalTokens)
	}
}

func TestGetStats_EmptyProject(t *testing.T) {
	s := openTestStore(t)

	stats, err := s.GetStats("unknown-hash")
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.TotalInjections != 0 {
		t.Errorf("TotalInjections = %d, want 0", stats.TotalInjections)
	}
	if stats.TotalTokens != 0 {
		t.Errorf("TotalTokens = %d, want 0", stats.TotalTokens)
	}
	if stats.AvgTokens != 0 {
		t.Errorf("AvgTokens = %v, want 0", stats.AvgTokens)
	}
}
