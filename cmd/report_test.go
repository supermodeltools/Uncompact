package cmd

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/supermodeltools/uncompact/internal/activitylog"
)

// TestFormatThousands verifies comma-separation across the three formatting branches:
// below 1K, 1K–999,999, and 1M and above.
func TestFormatThousands(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1,000"},
		{1001, "1,001"},
		{999999, "999,999"},
		{1000000, "1,000,000"},
		{1000001, "1,000,001"},
		{1_000_000_000, "1,000,000,000"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			got := formatThousands(tc.n)
			if got != tc.want {
				t.Errorf("formatThousands(%d) = %q, want %q", tc.n, got, tc.want)
			}
		})
	}
}

// TestFilterEntries_TimeWindow verifies that entries before the since cutoff are excluded
// and entries within the window are retained.
func TestFilterEntries_TimeWindow(t *testing.T) {
	now := time.Now().UTC()
	since := now.AddDate(0, 0, -7) // 7-day window

	entries := []activitylog.Entry{
		{Timestamp: now.AddDate(0, 0, -1), Project: "/a"}, // within window
		{Timestamp: now.AddDate(0, 0, -5), Project: "/b"}, // within window
		{Timestamp: now.AddDate(0, 0, -8), Project: "/c"}, // before window — excluded
	}

	got := filterEntries(entries, since, "")
	if len(got) != 2 {
		t.Fatalf("expected 2 entries within window, got %d", len(got))
	}
	for _, e := range got {
		if !e.Timestamp.After(since) && !e.Timestamp.Equal(since) {
			t.Errorf("entry with project %q has timestamp %v before since %v", e.Project, e.Timestamp, since)
		}
	}
}

// TestFilterEntries_AllTime verifies that a zero since returns all entries regardless of age.
func TestFilterEntries_AllTime(t *testing.T) {
	entries := []activitylog.Entry{
		{Timestamp: time.Now().UTC().AddDate(0, 0, -100), Project: "/old"},
		{Timestamp: time.Now().UTC().AddDate(0, 0, -1000), Project: "/ancient"},
	}

	got := filterEntries(entries, time.Time{}, "")
	if len(got) != 2 {
		t.Fatalf("expected 2 entries for all-time (zero since), got %d", len(got))
	}
}

// TestFilterEntries_ProjectFilter verifies that only entries matching the filterProject
// (after filepath.Clean normalization) are returned.
func TestFilterEntries_ProjectFilter(t *testing.T) {
	now := time.Now().UTC()
	entries := []activitylog.Entry{
		{Project: "/home/user/myproject", Timestamp: now},
		{Project: "/home/user/other", Timestamp: now},
		{Project: "/home/user/myproject/", Timestamp: now}, // trailing slash normalizes to same path
	}

	filterProject := filepath.Clean("/home/user/myproject")
	got := filterEntries(entries, time.Time{}, filterProject)

	// Both "/home/user/myproject" and "/home/user/myproject/" should match after Clean.
	if len(got) != 2 {
		t.Fatalf("expected 2 matching entries, got %d", len(got))
	}
	for _, e := range got {
		if filepath.Clean(e.Project) != filterProject {
			t.Errorf("unexpected entry with project %q", e.Project)
		}
	}
}

// TestFilterEntries_ProjectFilter_NoMatch verifies that a non-matching project filter
// returns zero results rather than silently returning all entries.
func TestFilterEntries_ProjectFilter_NoMatch(t *testing.T) {
	entries := []activitylog.Entry{
		{Project: "/home/user/other", Timestamp: time.Now().UTC()},
	}

	got := filterEntries(entries, time.Time{}, "/home/user/myproject")
	if len(got) != 0 {
		t.Fatalf("expected 0 entries for non-matching project filter, got %d", len(got))
	}
}

// TestFilterEntries_Empty verifies that filtering an empty log returns nil.
func TestFilterEntries_Empty(t *testing.T) {
	got := filterEntries(nil, time.Now().UTC(), "")
	if len(got) != 0 {
		t.Fatalf("expected 0 entries for empty log, got %d", len(got))
	}
}

// TestBuildReportData_SnapshotCount verifies that SessionSnapshotsSaved counts only entries
// where SessionSnapshotPresent == true.
func TestBuildReportData_SnapshotCount(t *testing.T) {
	entries := []activitylog.Entry{
		{Project: "/a", SessionSnapshotPresent: true, ContextBombSizeBytes: 100},
		{Project: "/b", SessionSnapshotPresent: false, ContextBombSizeBytes: 200},
		{Project: "/c", SessionSnapshotPresent: true, ContextBombSizeBytes: 300},
	}

	rpt := buildReportData(entries, "last 30 days")
	if rpt.SessionSnapshotsSaved != 2 {
		t.Errorf("SessionSnapshotsSaved = %d, want 2", rpt.SessionSnapshotsSaved)
	}
}

// TestBuildReportData_TotalBytes verifies that ContextBombSizeBytes values are summed.
func TestBuildReportData_TotalBytes(t *testing.T) {
	entries := []activitylog.Entry{
		{Project: "/a", ContextBombSizeBytes: 4000},
		{Project: "/b", ContextBombSizeBytes: 8000},
	}

	rpt := buildReportData(entries, "last 30 days")
	if rpt.TotalContextBombBytes != 12000 {
		t.Errorf("TotalContextBombBytes = %d, want 12000", rpt.TotalContextBombBytes)
	}
}

// TestBuildReportData_TokenEstimation verifies the bytes/4 token heuristic fallback.
func TestBuildReportData_TokenEstimation(t *testing.T) {
	entries := []activitylog.Entry{
		{Project: "/a", ContextBombSizeBytes: 4000},
	}

	rpt := buildReportData(entries, "last 30 days")
	if rpt.TotalTokens != 1000 {
		t.Errorf("TotalTokens = %d, want 1000 (4000 bytes / 4 fallback)", rpt.TotalTokens)
	}
	if rpt.TokensExact {
		t.Errorf("TokensExact = true, want false for byte-estimated fallback")
	}
}

// TestBuildReportData_HoursEstimation verifies the 10-minutes-per-compaction heuristic.
func TestBuildReportData_HoursEstimation(t *testing.T) {
	entries := []activitylog.Entry{
		{Project: "/a"},
		{Project: "/b"},
		{Project: "/c"},
	}

	rpt := buildReportData(entries, "last 30 days")
	want := 3.0 * 10.0 / 60.0 // 3 compactions × 10 min ÷ 60 = 0.5 hours
	if rpt.EstimatedHoursSaved != want {
		t.Errorf("EstimatedHoursSaved = %f, want %f", rpt.EstimatedHoursSaved, want)
	}
}

// TestBuildReportData_CompactionCount verifies that Compactions counts only entries where
// SessionSnapshotPresent == true, while ContextBombsDelivered counts all filtered entries.
func TestBuildReportData_CompactionCount(t *testing.T) {
	entries := []activitylog.Entry{
		{Project: "/a", SessionSnapshotPresent: true},
		{Project: "/b", SessionSnapshotPresent: false},
		{Project: "/c", SessionSnapshotPresent: true},
	}

	rpt := buildReportData(entries, "last 30 days")
	if rpt.Compactions != 2 {
		t.Errorf("Compactions = %d, want 2 (snapshot-present entries only)", rpt.Compactions)
	}
	if rpt.ContextBombsDelivered != 3 {
		t.Errorf("ContextBombsDelivered = %d, want 3 (all entries)", rpt.ContextBombsDelivered)
	}
	if rpt.Compactions == rpt.ContextBombsDelivered {
		t.Errorf("Compactions (%d) should not equal ContextBombsDelivered (%d) when only some entries have snapshots", rpt.Compactions, rpt.ContextBombsDelivered)
	}
}

// TestBuildReportData_Empty verifies that an empty entry slice produces zero counts and no last-compaction.
func TestBuildReportData_Empty(t *testing.T) {
	rpt := buildReportData(nil, "last 30 days")
	if rpt.Compactions != 0 {
		t.Errorf("Compactions = %d, want 0", rpt.Compactions)
	}
	if rpt.LastCompaction != nil {
		t.Errorf("LastCompaction = %v, want nil", rpt.LastCompaction)
	}
}

// TestBuildReportData_WindowLabel verifies that the window label is preserved verbatim.
func TestBuildReportData_WindowLabel(t *testing.T) {
	rpt := buildReportData(nil, "all time")
	if rpt.Window != "all time" {
		t.Errorf("Window = %q, want %q", rpt.Window, "all time")
	}
}
