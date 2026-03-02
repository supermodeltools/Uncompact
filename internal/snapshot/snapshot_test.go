package snapshot

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteRead_RoundTrip(t *testing.T) {
	dir := t.TempDir()

	snap := &SessionSnapshot{
		Timestamp: time.Now().UTC().Truncate(time.Second),
		Content:   "## Session State\n\nSome content here.",
	}

	if err := Write(dir, snap); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := ReadWithTTL(dir, time.Hour)
	if err != nil {
		t.Fatalf("ReadWithTTL: %v", err)
	}
	if got == nil {
		t.Fatal("ReadWithTTL returned nil, expected snapshot")
	}
	if got.Content != snap.Content {
		t.Errorf("Content mismatch: got %q, want %q", got.Content, snap.Content)
	}
	if !got.Timestamp.Equal(snap.Timestamp) {
		t.Errorf("Timestamp mismatch: got %v, want %v", got.Timestamp, snap.Timestamp)
	}
}

func TestReadWithTTL_Expired(t *testing.T) {
	dir := t.TempDir()

	snap := &SessionSnapshot{
		Timestamp: time.Now().UTC().Add(-2 * time.Hour),
		Content:   "old content",
	}

	if err := Write(dir, snap); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := ReadWithTTL(dir, time.Hour)
	if err != nil {
		t.Fatalf("ReadWithTTL: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for expired snapshot, got %+v", got)
	}
}

func TestReadWithTTL_TruncatedHeader(t *testing.T) {
	dir := t.TempDir()

	// Write a file with a truncated header: prefix present but suffix missing.
	truncated := headerPrefix + "2024-01-01T00:00:00Z\n## Some content"
	p := Path(dir)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(p, []byte(truncated), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := ReadWithTTL(dir, time.Hour)
	if err != nil {
		t.Fatalf("ReadWithTTL: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for truncated header, got %+v", got)
	}
}

func TestReadWithTTL_MalformedTimestamp(t *testing.T) {
	dir := t.TempDir()

	// Write a file with a structurally valid header but an invalid timestamp value.
	malformed := headerPrefix + "not-a-timestamp" + headerSuffix + "\n## Some content"
	p := Path(dir)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(p, []byte(malformed), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := ReadWithTTL(dir, time.Hour)
	if err != nil {
		t.Fatalf("ReadWithTTL: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for malformed timestamp, got %+v", got)
	}
}

func TestReadWithTTL_ZeroTTL_SkipsExpiry(t *testing.T) {
	dir := t.TempDir()

	snap := &SessionSnapshot{
		Timestamp: time.Now().UTC().Add(-100 * time.Hour),
		Content:   "very old content",
	}

	if err := Write(dir, snap); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Zero TTL should skip expiry check.
	got, err := ReadWithTTL(dir, 0)
	if err != nil {
		t.Fatalf("ReadWithTTL: %v", err)
	}
	if got == nil {
		t.Fatal("expected snapshot with zero TTL, got nil")
	}
	if got.Content != snap.Content {
		t.Errorf("Content mismatch: got %q, want %q", got.Content, snap.Content)
	}
}

func TestRead_NonExistent(t *testing.T) {
	dir := t.TempDir()

	got, err := Read(dir)
	if err != nil {
		t.Fatalf("Read on nonexistent file: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for nonexistent snapshot, got %+v", got)
	}
}

func TestWrite_CreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "a", "b", "c")

	snap := &SessionSnapshot{
		Timestamp: time.Now().UTC(),
		Content:   "content",
	}

	if err := Write(nested, snap); err != nil {
		t.Fatalf("Write to nested dir: %v", err)
	}

	if _, err := os.Stat(Path(nested)); err != nil {
		t.Errorf("snapshot file not created: %v", err)
	}
}

// TestReadWithTTL_NoHeader_ConcurrentWrite_PreservesFile verifies that the TOCTOU
// guard works: if a concurrent snapshot.Write atomically replaces the file between
// ReadWithTTL's initial os.Stat and its os.Remove, the fresh snapshot is preserved.
func TestReadWithTTL_NoHeader_ConcurrentWrite_PreservesFile(t *testing.T) {
	dir := t.TempDir()
	p := Path(dir)

	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Write an expired headerless snapshot (simulates a legacy file).
	if err := os.WriteFile(p, []byte("## Old Session State"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	backdated := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(p, backdated, backdated); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	// Inject a concurrent write that fires after ReadWithTTL has captured the old
	// mtime but before it reaches os.Remove — this is the TOCTOU window.
	testHookAfterHeaderlessStat = func() {
		testHookAfterHeaderlessStat = nil // one-shot
		if err := Write(dir, &SessionSnapshot{
			Timestamp: time.Now().UTC(),
			Content:   "## Fresh Session State",
		}); err != nil {
			t.Errorf("concurrent Write: %v", err)
		}
	}
	t.Cleanup(func() { testHookAfterHeaderlessStat = nil })

	// ReadWithTTL must detect that the mtime advanced (the hook replaced the file)
	// and skip os.Remove, leaving the fresh snapshot intact.
	if _, err := ReadWithTTL(dir, time.Hour); err != nil {
		t.Fatalf("ReadWithTTL: %v", err)
	}

	if _, statErr := os.Stat(p); os.IsNotExist(statErr) {
		t.Error("ReadWithTTL deleted the fresh snapshot written by a concurrent Write; TOCTOU guard is missing")
	}
}

func TestReadWithTTL_NoHeader_ContentPreserved(t *testing.T) {
	dir := t.TempDir()

	// Write raw content without the header — should be read back as-is.
	raw := "## Handcrafted snapshot\n\nSome text."
	p := Path(dir)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(p, []byte(raw), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := ReadWithTTL(dir, time.Hour)
	if err != nil {
		t.Fatalf("ReadWithTTL: %v", err)
	}
	if got == nil {
		t.Fatal("expected snapshot, got nil")
	}
	// Content should match (TrimSpace is applied by ReadWithTTL).
	if got.Content != raw {
		t.Errorf("Content mismatch: got %q, want %q", got.Content, raw)
	}
}
