package activitylog

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/supermodeltools/uncompact/internal/config"
)

const (
	maxLogSize = 5 * 1024 * 1024 // 5MB hard cap before rotation
	rotateKeep = 2 * 1024 * 1024 // bytes to retain after rotation
)

// Entry records a single PostCompact event.
type Entry struct {
	Timestamp              time.Time `json:"timestamp"`
	Project                string    `json:"project"`
	ContextBombSizeBytes   int       `json:"context_bomb_size_bytes"`
	SessionSnapshotPresent bool      `json:"session_snapshot_present"`
}

// LogPath returns the platform-appropriate path for the activity log.
func LogPath() (string, error) {
	dataDir, err := config.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "activity.log"), nil
}

// Append writes an Entry to the activity log, rotating if the file exceeds maxLogSize.
// Errors are non-fatal in the caller; callers should silence them.
func Append(e Entry) error {
	path, err := LogPath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}

	// Acquire a cross-process exclusive lock that covers the full
	// stat→rotate→append sequence, preventing TOCTOU data loss when
	// multiple processes (e.g. uncompact run + uncompact pregen) write
	// concurrently. A separate lock file avoids interfering with O_APPEND.
	lf, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer lf.Close()
	if err := lockFile(lf); err != nil {
		return err
	}
	defer unlockFile(lf)

	// Rotate before writing if the log is too large.
	if info, err := os.Stat(path); err == nil && info.Size() > maxLogSize {
		_ = rotate(path)
	}

	data, err := json.Marshal(e)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.Write(append(data, '\n'))
	return err
}

// rotate truncates the log to the last rotateKeep bytes, aligned to a line boundary.
// It uses an atomic temp-file + rename pattern so a mid-write crash cannot corrupt the log.
func rotate(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(data) <= rotateKeep {
		return nil
	}
	offset := len(data) - rotateKeep
	if idx := bytes.IndexByte(data[offset:], '\n'); idx >= 0 {
		offset += idx + 1
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data[offset:], 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ReadAll reads all entries from the activity log.
// Returns nil, nil if the log file does not yet exist.
func ReadAll() ([]Entry, error) {
	path, err := LogPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var entries []Entry
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			continue // skip malformed lines
		}
		entries = append(entries, e)
	}
	return entries, nil
}
