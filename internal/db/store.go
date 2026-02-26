package db

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"github.com/supermodeltools/uncompact/internal/api"
)

// Store wraps the SQLite database for caching graph data.
type Store struct {
	db *sql.DB
}

// InjectionEvent records a single context injection.
type InjectionEvent struct {
	WorkspaceHash  string
	TokensInjected int
	CacheHit       bool
	InjectedAt     time.Time
}

// Stats holds aggregate statistics.
type Stats struct {
	TotalInjections int
	TotalTokens     int
	CacheHits       int
	APICalls        int
}

const schema = `
CREATE TABLE IF NOT EXISTS graph_cache (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	workspace_hash TEXT NOT NULL,
	data           TEXT NOT NULL,
	fetched_at     DATETIME NOT NULL,
	expires_at     DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_cache_workspace ON graph_cache(workspace_hash, fetched_at DESC);

CREATE TABLE IF NOT EXISTS injection_log (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	workspace_hash TEXT NOT NULL,
	tokens         INTEGER NOT NULL,
	cache_hit      INTEGER NOT NULL,
	injected_at    DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_log_workspace ON injection_log(workspace_hash, injected_at DESC);
`

// Open opens or creates the SQLite database at path.
func Open(path string) (*Store, error) {
	if path == "" {
		path = defaultPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}

	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// GetCachedGraph returns a non-expired cached graph for the workspace, or nil.
func (s *Store) GetCachedGraph(workspaceHash string) (*api.GraphOutput, error) {
	row := s.db.QueryRow(`
		SELECT data FROM graph_cache
		WHERE workspace_hash = ? AND expires_at > ?
		ORDER BY fetched_at DESC LIMIT 1`,
		workspaceHash, time.Now())

	var data string
	if err := row.Scan(&data); err != nil {
		return nil, err
	}

	var g api.GraphOutput
	if err := json.Unmarshal([]byte(data), &g); err != nil {
		return nil, err
	}
	return &g, nil
}

// GetStaleCachedGraph returns the most recent cached graph regardless of expiry.
func (s *Store) GetStaleCachedGraph(workspaceHash string) (*api.GraphOutput, error) {
	row := s.db.QueryRow(`
		SELECT data FROM graph_cache
		WHERE workspace_hash = ?
		ORDER BY fetched_at DESC LIMIT 1`,
		workspaceHash)

	var data string
	if err := row.Scan(&data); err != nil {
		return nil, err
	}

	var g api.GraphOutput
	if err := json.Unmarshal([]byte(data), &g); err != nil {
		return nil, err
	}
	return &g, nil
}

// CacheGraph stores a graph output with TTL.
func (s *Store) CacheGraph(workspaceHash string, g *api.GraphOutput) error {
	if g.ExpiresAt.IsZero() {
		g.ExpiresAt = time.Now().Add(15 * time.Minute)
	}

	data, err := json.Marshal(g)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(`
		INSERT INTO graph_cache (workspace_hash, data, fetched_at, expires_at)
		VALUES (?, ?, ?, ?)`,
		workspaceHash, string(data), g.FetchedAt, g.ExpiresAt)
	return err
}

// LastInjectionTime returns the time of the most recent injection for this workspace.
func (s *Store) LastInjectionTime(workspaceHash string) (time.Time, error) {
	row := s.db.QueryRow(`
		SELECT injected_at FROM injection_log
		WHERE workspace_hash = ?
		ORDER BY injected_at DESC LIMIT 1`,
		workspaceHash)

	var t time.Time
	if err := row.Scan(&t); err != nil {
		return time.Time{}, err
	}
	return t, nil
}

// LogInjection records a context injection event.
func (s *Store) LogInjection(e InjectionEvent) error {
	cacheHit := 0
	if e.CacheHit {
		cacheHit = 1
	}
	_, err := s.db.Exec(`
		INSERT INTO injection_log (workspace_hash, tokens, cache_hit, injected_at)
		VALUES (?, ?, ?, ?)`,
		e.WorkspaceHash, e.TokensInjected, cacheHit, e.InjectedAt)
	return err
}

// GetLogs returns the most recent injection log entries.
func (s *Store) GetLogs(limit int) ([]InjectionEvent, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query(`
		SELECT workspace_hash, tokens, cache_hit, injected_at
		FROM injection_log
		ORDER BY injected_at DESC LIMIT ?`,
		limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []InjectionEvent
	for rows.Next() {
		var e InjectionEvent
		var cacheHit int
		if err := rows.Scan(&e.WorkspaceHash, &e.TokensInjected, &cacheHit, &e.InjectedAt); err != nil {
			return nil, err
		}
		e.CacheHit = cacheHit == 1
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// GetStats returns aggregate statistics.
func (s *Store) GetStats() (*Stats, error) {
	row := s.db.QueryRow(`
		SELECT
			COUNT(*) as total,
			COALESCE(SUM(tokens), 0) as total_tokens,
			COALESCE(SUM(cache_hit), 0) as cache_hits,
			COALESCE(SUM(1 - cache_hit), 0) as api_calls
		FROM injection_log`)

	var st Stats
	if err := row.Scan(&st.TotalInjections, &st.TotalTokens, &st.CacheHits, &st.APICalls); err != nil {
		return nil, err
	}
	return &st, nil
}

// ClearWorkspace removes all cached data for a specific workspace.
func (s *Store) ClearWorkspace(workspaceHash string) error {
	_, err := s.db.Exec(`DELETE FROM graph_cache WHERE workspace_hash = ?`, workspaceHash)
	return err
}

// ClearAll removes all cached graph data.
func (s *Store) ClearAll() error {
	_, err := s.db.Exec(`DELETE FROM graph_cache`)
	return err
}

// Prune removes expired cache entries and returns count removed.
func (s *Store) Prune() (int, error) {
	result, err := s.db.Exec(`
		DELETE FROM graph_cache WHERE expires_at < ?`,
		time.Now().Add(-30*24*time.Hour))
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

// HashWorkspace creates a stable hash for a workspace path.
func HashWorkspace(path string) string {
	h := sha256.Sum256([]byte(path))
	return fmt.Sprintf("%x", h[:16])
}

func defaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "uncompact", "cache.db")
}
