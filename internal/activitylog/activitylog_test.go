package activitylog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// setLogDir redirects XDG_DATA_HOME to a fresh temporary directory for the
// duration of the test and returns the temp dir path. On Linux, DataDir()
// honours XDG_DATA_HOME, so this cleanly isolates each test from the real
// data directory without touching production files.
func setLogDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	return dir
}

// logFilePath derives the activity.log path from a redirected data dir.
func logFilePath(dataHome string) string {
	return filepath.Join(dataHome, "uncompact", "activity.log")
}

// makeJSONLine returns one valid JSON-encoded Entry followed by a newline.
func makeJSONLine(t *testing.T, project string) []byte {
	t.Helper()
	e := Entry{Timestamp: time.Now().UTC(), Project: project}
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return append(b, '\n')
}

// TestAppend_RoundTrip verifies that an entry written with Append is returned
// verbatim by ReadAll.
func TestAppend_RoundTrip(t *testing.T) {
	setLogDir(t)

	want := Entry{
		Timestamp:              time.Now().UTC().Truncate(time.Second),
		Project:                "test-project",
		ContextBombSizeBytes:   12345,
		SessionSnapshotPresent: true,
	}

	if err := Append(want); err != nil {
		t.Fatalf("Append: %v", err)
	}

	entries, err := ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	got := entries[0]
	if !got.Timestamp.Equal(want.Timestamp) {
		t.Errorf("Timestamp: got %v, want %v", got.Timestamp, want.Timestamp)
	}
	if got.Project != want.Project {
		t.Errorf("Project: got %q, want %q", got.Project, want.Project)
	}
	if got.ContextBombSizeBytes != want.ContextBombSizeBytes {
		t.Errorf("ContextBombSizeBytes: got %d, want %d", got.ContextBombSizeBytes, want.ContextBombSizeBytes)
	}
	if got.SessionSnapshotPresent != want.SessionSnapshotPresent {
		t.Errorf("SessionSnapshotPresent: got %v, want %v", got.SessionSnapshotPresent, want.SessionSnapshotPresent)
	}
}

// TestReadAll_NoFile verifies that ReadAll returns nil, nil when the log file
// does not yet exist.
func TestReadAll_NoFile(t *testing.T) {
	setLogDir(t)

	entries, err := ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: unexpected error: %v", err)
	}
	if entries != nil {
		t.Errorf("expected nil entries, got %v", entries)
	}
}

// TestReadAll_SkipsMalformed verifies that ReadAll silently ignores lines that
// are not valid JSON without returning an error, and still parses valid lines.
func TestReadAll_SkipsMalformed(t *testing.T) {
	dir := setLogDir(t)

	logPath := logFilePath(dir)
	if err := os.MkdirAll(filepath.Dir(logPath), 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	good := Entry{
		Timestamp: time.Now().UTC().Truncate(time.Second),
		Project:   "ok",
	}
	goodJSON, _ := json.Marshal(good)

	// Two valid lines surrounding one malformed line.
	content := fmt.Sprintf("%s\nnot-valid-json\n%s\n", goodJSON, goodJSON)
	if err := os.WriteFile(logPath, []byte(content), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	entries, err := ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 valid entries (malformed line skipped), got %d", len(entries))
	}
}

// TestRotate_TriggeredAt5MB verifies that calling Append on a log already
// exceeding 5 MB triggers rotation, leaving the file well below maxLogSize.
func TestRotate_TriggeredAt5MB(t *testing.T) {
	dir := setLogDir(t)

	logPath := logFilePath(dir)
	if err := os.MkdirAll(filepath.Dir(logPath), 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Fill the log file to just over maxLogSize with valid JSON lines.
	line := makeJSONLine(t, "padding")
	var buf bytes.Buffer
	for buf.Len() <= maxLogSize {
		buf.Write(line)
	}
	if err := os.WriteFile(logPath, buf.Bytes(), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Append a new entry; rotation should fire because file > maxLogSize.
	if err := Append(Entry{Project: "trigger", Timestamp: time.Now().UTC()}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	// After rotation the file holds ~rotateKeep bytes plus the new entry —
	// well below maxLogSize.
	if info.Size() >= int64(maxLogSize) {
		t.Errorf("expected file size < %d after rotation, got %d", maxLogSize, info.Size())
	}
}

// TestRotate_PreservesLineBoundary verifies that after rotation every line in
// the retained tail is a complete, valid JSON object — rotate never splits a
// log entry mid-line.
func TestRotate_PreservesLineBoundary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "activity.log")

	// Build a file large enough to require rotation where the 2 MB split
	// point falls mid-line, forcing rotate to advance to the next newline.
	line := makeJSONLine(t, "boundary-test")
	var buf bytes.Buffer
	for buf.Len() <= rotateKeep+len(line) {
		buf.Write(line)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := rotate(path); err != nil {
		t.Fatalf("rotate: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile after rotate: %v", err)
	}

	// Every non-empty line in the retained data must be valid JSON.
	for i, raw := range bytes.Split(data, []byte{'\n'}) {
		raw = bytes.TrimSpace(raw)
		if len(raw) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(raw, &e); err != nil {
			t.Errorf("line %d is not valid JSON after rotation: %v — got %q", i, err, raw)
		}
	}
}

// TestAppend_Concurrent verifies that concurrent Append calls from multiple
// goroutines produce no lost or corrupted entries. The total payload is kept
// well below maxLogSize to avoid rotation complicating the count check.
func TestAppend_Concurrent(t *testing.T) {
	setLogDir(t)

	const goroutines = 8
	const appendsPerGoroutine = 20

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < appendsPerGoroutine; i++ {
				e := Entry{
					Timestamp: time.Now().UTC(),
					Project:   fmt.Sprintf("goroutine-%d-iter-%d", id, i),
				}
				if err := Append(e); err != nil {
					t.Errorf("Append (goroutine %d, iter %d): %v", id, i, err)
				}
			}
		}(g)
	}
	wg.Wait()

	entries, err := ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	want := goroutines * appendsPerGoroutine
	if len(entries) != want {
		t.Errorf("expected %d entries, got %d (concurrent writes lost or duplicated data)", want, len(entries))
	}

	for i, e := range entries {
		if e.Project == "" {
			t.Errorf("entry %d has empty Project field (possible JSON corruption)", i)
		}
	}
}
