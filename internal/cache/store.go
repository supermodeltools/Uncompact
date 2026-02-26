package cache

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/supermodeltools/uncompact/internal/api"
	_ "modernc.org/sqlite"
)

const (
	defaultTTL     = 15 * time.Minute
	defaultMaxAge  = 30 * 24 * time.Hour // 30 days
	schemaVersion  = 1
)

// Store is the SQLite-backed cache for Uncompact.
type Store struct {
	db  *sql.DB
	ttl time.Duration
}

// InjectionLog is a record of a context bomb injection.
type InjectionLog struct {
	ID          int64
	ProjectHash string
	ProjectName string
	Tokens      int
	Source      string // "api" or "cache"
	StaleAt     *time.Time
	CreatedAt   time.Time
}

// Open opens (or creates) the SQLite store at dbPath.
func Open(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath+"?_journal=WAL&_timeout=5000&_fk=true")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	s := &Store{db: db, ttl: defaultTTL}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrating database: %w", err)
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// migrate runs schema migrations.
func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		PRAGMA journal_mode=WAL;

		CREATE TABLE IF NOT EXISTS schema_version (
			version INTEGER PRIMARY KEY,
			applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS graph_cache (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_hash TEXT NOT NULL UNIQUE,
			project_name TEXT NOT NULL,
			graph_json TEXT NOT NULL,
			fetched_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			expires_at TIMESTAMP NOT NULL
		);

		CREATE TABLE IF NOT EXISTS injection_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_hash TEXT NOT NULL,
			project_name TEXT NOT NULL,
			tokens INTEGER NOT NULL DEFAULT 0,
			source TEXT NOT NULL DEFAULT 'api',
			stale_at TIMESTAMP,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_graph_cache_hash ON graph_cache(project_hash);
		CREATE INDEX IF NOT EXISTS idx_graph_cache_expires ON graph_cache(expires_at);
		CREATE INDEX IF NOT EXISTS idx_injection_log_project ON injection_log(project_hash);
		CREATE INDEX IF NOT EXISTS idx_injection_log_created ON injection_log(created_at);
	`)
	return err
}

// Get retrieves a cached graph for the given project hash.
// Returns (graph, isFresh, nil) — graph may be stale but non-nil.
func (s *Store) Get(projectHash string) (*api.ProjectGraph, bool, error) {
	row := s.db.QueryRow(`
		SELECT graph_json, expires_at
		FROM graph_cache
		WHERE project_hash = ?`,
		projectHash,
	)

	var graphJSON string
	var expiresAt time.Time
	if err := row.Scan(&graphJSON, &expiresAt); err == sql.ErrNoRows {
		return nil, false, nil
	} else if err != nil {
		return nil, false, err
	}

	var graph api.ProjectGraph
	if err := json.Unmarshal([]byte(graphJSON), &graph); err != nil {
		return nil, false, err
	}

	isFresh := time.Now().Before(expiresAt)
	return &graph, isFresh, nil
}

// Set stores a graph for the given project hash with the default TTL.
func (s *Store) Set(projectHash, projectName string, graph *api.ProjectGraph) error {
	graphJSON, err := json.Marshal(graph)
	if err != nil {
		return err
	}

	now := time.Now()
	expiresAt := now.Add(s.ttl)

	_, err = s.db.Exec(`
		INSERT INTO graph_cache (project_hash, project_name, graph_json, fetched_at, expires_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(project_hash) DO UPDATE SET
			project_name = excluded.project_name,
			graph_json = excluded.graph_json,
			fetched_at = excluded.fetched_at,
			expires_at = excluded.expires_at`,
		projectHash, projectName, string(graphJSON), now, expiresAt,
	)
	return err
}

// LogInjection records a context bomb injection event.
func (s *Store) LogInjection(projectHash, projectName string, tokens int, source string, staleAt *time.Time) error {
	_, err := s.db.Exec(`
		INSERT INTO injection_log (project_hash, project_name, tokens, source, stale_at)
		VALUES (?, ?, ?, ?, ?)`,
		projectHash, projectName, tokens, source, staleAt,
	)
	return err
}

// RecentLogs returns the most recent injection log entries.
func (s *Store) RecentLogs(limit int) ([]InjectionLog, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query(`
		SELECT id, project_hash, project_name, tokens, source, stale_at, created_at
		FROM injection_log
		ORDER BY created_at DESC
		LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []InjectionLog
	for rows.Next() {
		var l InjectionLog
		var staleAt sql.NullTime
		if err := rows.Scan(&l.ID, &l.ProjectHash, &l.ProjectName, &l.Tokens, &l.Source, &staleAt, &l.CreatedAt); err != nil {
			return nil, err
		}
		if staleAt.Valid {
			l.StaleAt = &staleAt.Time
		}
		logs = append(logs, l)
	}
	return logs, rows.Err()
}

// Stats returns aggregate stats for the injection log.
type Stats struct {
	TotalInjections int
	APIFetches      int
	CacheHits       int
	TotalTokens     int
	AvgTokens       float64
}

// GetStats returns aggregate injection statistics.
func (s *Store) GetStats(projectHash string) (*Stats, error) {
	query := `
		SELECT
			COUNT(*) as total,
			SUM(CASE WHEN source = 'api' THEN 1 ELSE 0 END) as api_fetches,
			SUM(CASE WHEN source = 'cache' OR source = 'stale_cache' THEN 1 ELSE 0 END) as cache_hits,
			SUM(tokens) as total_tokens,
			AVG(tokens) as avg_tokens
		FROM injection_log`
	args := []interface{}{}
	if projectHash != "" {
		query += " WHERE project_hash = ?"
		args = append(args, projectHash)
	}

	var st Stats
	var avgTokens sql.NullFloat64
	err := s.db.QueryRow(query, args...).Scan(
		&st.TotalInjections, &st.APIFetches, &st.CacheHits, &st.TotalTokens, &avgTokens,
	)
	if err != nil {
		return nil, err
	}
	if avgTokens.Valid {
		st.AvgTokens = avgTokens.Float64
	}
	return &st, nil
}

// Prune removes graph cache entries older than maxAge and injection logs older than 90 days.
func (s *Store) Prune() error {
	cutoff := time.Now().Add(-defaultMaxAge)
	if _, err := s.db.Exec(`DELETE FROM graph_cache WHERE fetched_at < ?`, cutoff); err != nil {
		return err
	}
	logCutoff := time.Now().Add(-90 * 24 * time.Hour)
	_, err := s.db.Exec(`DELETE FROM injection_log WHERE created_at < ?`, logCutoff)
	return err
}

// ClearProject removes all cached data for a project.
func (s *Store) ClearProject(projectHash string) error {
	_, err := s.db.Exec(`DELETE FROM graph_cache WHERE project_hash = ?`, projectHash)
	return err
}

// ClearAll removes all cached data.
func (s *Store) ClearAll() error {
	_, err := s.db.Exec(`DELETE FROM graph_cache`)
	return err
}

// LastInjection returns the most recent injection for a project.
func (s *Store) LastInjection(projectHash string) (*InjectionLog, error) {
	row := s.db.QueryRow(`
		SELECT id, project_hash, project_name, tokens, source, stale_at, created_at
		FROM injection_log
		WHERE project_hash = ?
		ORDER BY created_at DESC
		LIMIT 1`,
		projectHash,
	)

	var l InjectionLog
	var staleAt sql.NullTime
	if err := row.Scan(&l.ID, &l.ProjectHash, &l.ProjectName, &l.Tokens, &l.Source, &staleAt, &l.CreatedAt); err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	if staleAt.Valid {
		l.StaleAt = &staleAt.Time
	}
	return &l, nil
}
