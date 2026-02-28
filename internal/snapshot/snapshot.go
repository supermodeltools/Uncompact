package snapshot

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	snapshotDir      = ".uncompact"
	snapshotFilename = "session-snapshot.md"
	DefaultTTL       = 24 * time.Hour
	headerPrefix     = "<!-- uncompact-snapshot: "
	headerSuffix     = " -->"
)

// SessionSnapshot holds the captured session state before compaction.
type SessionSnapshot struct {
	Timestamp time.Time
	Content   string // Markdown content (human-readable)
}

// Path returns the full path to the snapshot file for the given project root.
func Path(projectRoot string) string {
	return filepath.Join(projectRoot, snapshotDir, snapshotFilename)
}

// Write persists a session snapshot to the project's .uncompact directory.
func Write(projectRoot string, snap *SessionSnapshot) error {
	dir := filepath.Join(projectRoot, snapshotDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating snapshot directory: %w", err)
	}

	ts := snap.Timestamp.UTC().Format(time.RFC3339)
	content := headerPrefix + ts + headerSuffix + "\n" + snap.Content

	tmp := Path(projectRoot) + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, Path(projectRoot))
}

// Read loads the session snapshot if it exists and is within the default TTL.
// Returns nil if no snapshot exists or it has expired.
func Read(projectRoot string) (*SessionSnapshot, error) {
	return ReadWithTTL(projectRoot, DefaultTTL)
}

// ReadWithTTL loads the session snapshot with a custom TTL.
// Returns nil (no error) if the file does not exist or has expired.
func ReadWithTTL(projectRoot string, ttl time.Duration) (*SessionSnapshot, error) {
	data, err := os.ReadFile(Path(projectRoot))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading snapshot: %w", err)
	}

	content := string(data)
	var timestamp time.Time

	// Parse optional timestamp from header comment
	if strings.HasPrefix(content, headerPrefix) {
		end := strings.Index(content, headerSuffix)
		if end < 0 {
			// Header prefix found but suffix is missing — file is truncated or corrupt.
			// Treat as unreadable to prevent stale/partial content from being injected.
			return nil, nil
		}
		tsStr := content[len(headerPrefix):end]
		t, parseErr := time.Parse(time.RFC3339, tsStr)
		if parseErr != nil {
			// Header present but timestamp is malformed — treat as corrupt/expired.
			return nil, nil
		}
		timestamp = t
		// Strip header line
		if nl := strings.Index(content, "\n"); nl >= 0 {
			content = content[nl+1:]
		} else {
			content = ""
		}
	}

	// Enforce TTL
	if !timestamp.IsZero() && ttl > 0 && time.Since(timestamp) > ttl {
		_ = os.Remove(Path(projectRoot)) // clean up stale file
		return nil, nil
	}

	return &SessionSnapshot{
		Timestamp: timestamp,
		Content:   strings.TrimSpace(content),
	}, nil
}

// Clear removes the session snapshot file.
func Clear(projectRoot string) error {
	err := os.Remove(Path(projectRoot))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
