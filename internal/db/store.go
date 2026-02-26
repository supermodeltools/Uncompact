package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Store manages the local SQLite cache of graph outputs.
type Store struct {
	db *sql.DB
}

// GraphRecord is a stored graph snapshot.
type GraphRecord struct {
	ID              int64
	ProjectID       string
	FetchedAt       time.Time
	TTL             time.Duration
	Source          string
	EstimatedTokens int
	Data            []byte
}

// Stats holds aggregate usage statistics.
type Stats struct {
	TotalInjections int64
	CacheHits       int64
	APICalls        int64
	AvgTokens       int64
	TotalTokens     int64
}

// CacheHitRate returns the fraction of injections served from cache.
func (s *Stats) CacheHitRate() float64 {
	if s.TotalInjections == 0 {
		return 0
	}
	return float64(s.CacheHits) / float64(s.TotalInjections)
}

// LogEntry is a single activity log record.
type LogEntry struct {
	ID        int64
	Timestamp time.Time
	Event     string
	Tokens    int
	Source    string
}

// Open opens (or creates) the SQLite database at the default path,
// or at the provided path if non-empty.
func Open(path string) (*Store, error) {
	if path == "" {
		var err error
		path, err = defaultDBPath()
		if err != nil {
			return nil, err
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("creating data directory: %w", err)
	}

	sqlDB, err := sql.Open("sqlite3", path+"?_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	store := &Store{db: sqlDB}
	if err := store.migrate(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}
	return store, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// SaveRecord stores a new graph snapshot, pruning old records if needed.
func (s *Store) SaveRecord(record *GraphRecord) error {
	_, err := s.db.Exec(`
		INSERT INTO graphs (project_id, fetched_at, ttl_seconds, source, estimated_tokens, data)
		VALUES (?, ?, ?, ?, ?, ?)`,
		record.ProjectID,
		record.FetchedAt.Unix(),
		int64(record.TTL.Seconds()),
		record.Source,
		record.EstimatedTokens,
		record.Data,
	)
	if err != nil {
		return fmt.Errorf("inserting graph: %w", err)
	}

	return s.pruneIfOverLimit()
}

// GetLatest returns the most recently stored graph record, or nil if none exists.
func (s *Store) GetLatest() (*GraphRecord, error) {
	row := s.db.QueryRow(`
		SELECT id, project_id, fetched_at, ttl_seconds, source, estimated_tokens, data
		FROM graphs
		ORDER BY fetched_at DESC
		LIMIT 1`)

	return scanRecord(row)
}

// GetStats returns aggregate statistics.
func (s *Store) GetStats() (*Stats, error) {
	var stats Stats
	err := s.db.QueryRow(`
		SELECT
			COALESCE(SUM(injection_count), 0),
			COALESCE(SUM(cache_hits), 0),
			COALESCE(SUM(api_calls), 0),
			COALESCE(AVG(avg_tokens), 0),
			COALESCE(SUM(total_tokens), 0)
		FROM stats`).Scan(
		&stats.TotalInjections,
		&stats.CacheHits,
		&stats.APICalls,
		&stats.AvgTokens,
		&stats.TotalTokens,
	)
	if err != nil {
		return nil, err
	}
	return &stats, nil
}

// GetLogs returns the most recent n log entries.
func (s *Store) GetLogs(n int) ([]*LogEntry, error) {
	rows, err := s.db.Query(`
		SELECT id, timestamp, event, tokens, source
		FROM logs
		ORDER BY timestamp DESC
		LIMIT ?`, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*LogEntry
	for rows.Next() {
		var e LogEntry
		var ts int64
		if err := rows.Scan(&e.ID, &ts, &e.Event, &e.Tokens, &e.Source); err != nil {
			return nil, err
		}
		e.Timestamp = time.Unix(ts, 0)
		entries = append(entries, &e)
	}
	return entries, rows.Err()
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_version (version INTEGER PRIMARY KEY);

		CREATE TABLE IF NOT EXISTS graphs (
			id               INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id       TEXT    NOT NULL DEFAULT '',
			fetched_at       INTEGER NOT NULL,
			ttl_seconds      INTEGER NOT NULL DEFAULT 3600,
			source           TEXT    NOT NULL DEFAULT 'api',
			estimated_tokens INTEGER NOT NULL DEFAULT 0,
			data             BLOB    NOT NULL
		);

		CREATE TABLE IF NOT EXISTS logs (
			id        INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp INTEGER NOT NULL,
			event     TEXT    NOT NULL,
			tokens    INTEGER NOT NULL DEFAULT 0,
			source    TEXT    NOT NULL DEFAULT ''
		);

		CREATE TABLE IF NOT EXISTS stats (
			id               INTEGER PRIMARY KEY DEFAULT 1,
			injection_count  INTEGER NOT NULL DEFAULT 0,
			cache_hits       INTEGER NOT NULL DEFAULT 0,
			api_calls        INTEGER NOT NULL DEFAULT 0,
			avg_tokens       INTEGER NOT NULL DEFAULT 0,
			total_tokens     INTEGER NOT NULL DEFAULT 0
		);

		INSERT OR IGNORE INTO stats (id) VALUES (1);
	`)
	return err
}

func (s *Store) pruneIfOverLimit() error {
	_, err := s.db.Exec(`
		DELETE FROM graphs
		WHERE id NOT IN (
			SELECT id FROM graphs ORDER BY fetched_at DESC LIMIT 1000
		)`)
	return err
}

func defaultDBPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not find home directory: %w", err)
	}
	return filepath.Join(home, ".uncompact", "cache.db"), nil
}

func scanRecord(row *sql.Row) (*GraphRecord, error) {
	var r GraphRecord
	var fetchedAt int64
	var ttlSeconds int64

	err := row.Scan(&r.ID, &r.ProjectID, &fetchedAt, &ttlSeconds, &r.Source, &r.EstimatedTokens, &r.Data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	r.FetchedAt = time.Unix(fetchedAt, 0)
	r.TTL = time.Duration(ttlSeconds) * time.Second
	return &r, nil
}
