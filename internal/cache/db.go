package cache

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const (
	DefaultTTLSeconds  = 900    // 15 minutes
	MaxStorageBytes    = 100_000_000 // 100 MB soft cap (enforced by pruning)
	PruneAfterDays     = 30
)

// DB wraps the SQLite database for Uncompact caching.
type DB struct {
	sql *sql.DB
}

// CacheEntry is a single cached graph result.
type CacheEntry struct {
	ProjectHash string
	GraphType   string
	ResponseJSON string
	CreatedAt   time.Time
}

// Open opens (or creates) the SQLite cache database.
func Open(dbPath string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		return nil, fmt.Errorf("creating cache dir: %w", err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening cache db: %w", err)
	}
	d := &DB{sql: db}
	if err := d.migrate(); err != nil {
		return nil, fmt.Errorf("migrating cache db: %w", err)
	}
	return d, nil
}

// Close closes the database.
func (d *DB) Close() error {
	return d.sql.Close()
}

// migrate runs embedded schema migrations.
func (d *DB) migrate() error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS graph_cache (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_hash TEXT NOT NULL,
			graph_type TEXT NOT NULL,
			response_json TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			ttl_seconds INTEGER NOT NULL DEFAULT 900,
			UNIQUE(project_hash, graph_type)
		)`,
		`CREATE TABLE IF NOT EXISTS injection_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_hash TEXT NOT NULL,
			token_count INTEGER,
			cache_hit INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
	}
	for i, migration := range migrations {
		var count int
		_ = d.sql.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, i).Scan(&count)
		if count > 0 {
			continue
		}
		if _, err := d.sql.Exec(migration); err != nil {
			return fmt.Errorf("migration %d: %w", i, err)
		}
		_, _ = d.sql.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, i)
	}
	return nil
}

// Get retrieves a cached entry if it exists and is within TTL.
// Returns nil if not found or expired.
func (d *DB) Get(projectHash, graphType string, ttlSeconds int) (*CacheEntry, error) {
	if ttlSeconds <= 0 {
		ttlSeconds = DefaultTTLSeconds
	}
	row := d.sql.QueryRow(`
		SELECT project_hash, graph_type, response_json, created_at
		FROM graph_cache
		WHERE project_hash = ? AND graph_type = ?
		  AND datetime(created_at, '+' || ttl_seconds || ' seconds') > CURRENT_TIMESTAMP
	`, projectHash, graphType)

	var entry CacheEntry
	var createdAtStr string
	if err := row.Scan(&entry.ProjectHash, &entry.GraphType, &entry.ResponseJSON, &createdAtStr); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("reading cache: %w", err)
	}
	entry.CreatedAt, _ = time.Parse("2006-01-02T15:04:05Z", createdAtStr)
	return &entry, nil
}

// Set stores or updates a cache entry.
func (d *DB) Set(projectHash, graphType string, data interface{}, ttlSeconds int) error {
	if ttlSeconds <= 0 {
		ttlSeconds = DefaultTTLSeconds
	}
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("serializing cache entry: %w", err)
	}
	_, err = d.sql.Exec(`
		INSERT INTO graph_cache (project_hash, graph_type, response_json, ttl_seconds)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(project_hash, graph_type)
		DO UPDATE SET response_json = excluded.response_json,
		              created_at = CURRENT_TIMESTAMP,
		              ttl_seconds = excluded.ttl_seconds
	`, projectHash, graphType, string(jsonBytes), ttlSeconds)
	return err
}

// GetStale returns the most recent cached entry regardless of TTL.
// Used as fallback when API is unavailable.
func (d *DB) GetStale(projectHash, graphType string) (*CacheEntry, error) {
	row := d.sql.QueryRow(`
		SELECT project_hash, graph_type, response_json, created_at
		FROM graph_cache
		WHERE project_hash = ? AND graph_type = ?
		ORDER BY created_at DESC
		LIMIT 1
	`, projectHash, graphType)

	var entry CacheEntry
	var createdAtStr string
	if err := row.Scan(&entry.ProjectHash, &entry.GraphType, &entry.ResponseJSON, &createdAtStr); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("reading stale cache: %w", err)
	}
	entry.CreatedAt, _ = time.Parse("2006-01-02T15:04:05Z", createdAtStr)
	return &entry, nil
}

// LogInjection records a context injection event.
func (d *DB) LogInjection(projectHash string, tokenCount int, cacheHit bool) error {
	hit := 0
	if cacheHit {
		hit = 1
	}
	_, err := d.sql.Exec(`
		INSERT INTO injection_log (project_hash, token_count, cache_hit)
		VALUES (?, ?, ?)
	`, projectHash, tokenCount, hit)
	return err
}

// LastInjection returns metadata about the most recent injection.
func (d *DB) LastInjection(projectHash string) (createdAt time.Time, tokenCount int, cacheHit bool, err error) {
	row := d.sql.QueryRow(`
		SELECT created_at, token_count, cache_hit
		FROM injection_log
		WHERE project_hash = ?
		ORDER BY created_at DESC
		LIMIT 1
	`, projectHash)
	var createdAtStr string
	var hit int
	if scanErr := row.Scan(&createdAtStr, &tokenCount, &hit); scanErr != nil {
		if scanErr == sql.ErrNoRows {
			return time.Time{}, 0, false, nil
		}
		err = scanErr
		return
	}
	createdAt, _ = time.Parse("2006-01-02T15:04:05Z", createdAtStr)
	cacheHit = hit == 1
	return
}

// ClearAll removes all cache entries for a project.
func (d *DB) ClearAll(projectHash string) error {
	_, err := d.sql.Exec(`DELETE FROM graph_cache WHERE project_hash = ?`, projectHash)
	return err
}

// ClearAllProjects removes all cache entries for all projects.
func (d *DB) ClearAllProjects() error {
	_, err := d.sql.Exec(`DELETE FROM graph_cache`)
	return err
}

// Prune removes entries older than PruneAfterDays.
func (d *DB) Prune() error {
	_, err := d.sql.Exec(`
		DELETE FROM graph_cache
		WHERE created_at < datetime('now', ? || ' days')
	`, fmt.Sprintf("-%d", PruneAfterDays))
	if err != nil {
		return err
	}
	// Also prune logs older than 90 days
	_, err = d.sql.Exec(`DELETE FROM injection_log WHERE created_at < datetime('now', '-90 days')`)
	return err
}
