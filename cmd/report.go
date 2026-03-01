package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/spf13/cobra"
	"github.com/supermodeltools/uncompact/internal/activitylog"
	"github.com/supermodeltools/uncompact/internal/cache"
	"github.com/supermodeltools/uncompact/internal/config"
	"github.com/supermodeltools/uncompact/internal/project"
)

var (
	reportDays    int
	reportProject string
	reportJSON    bool
	reportAllTime bool
)

var reportCmd = &cobra.Command{
	Use:   "report",
	Short: "Show a summary of Uncompact activity and value delivered",
	Long: `Report reads the local activity log and produces a human-readable summary
of compaction events, context restored, and estimated time saved.

Use --json for machine-readable output, --days to change the time window,
and --project to filter to a specific directory.`,
	RunE: reportHandler,
}

func init() {
	rootCmd.AddCommand(reportCmd)
	reportCmd.Flags().IntVar(&reportDays, "days", 30, "Number of days to include in the report")
	reportCmd.Flags().StringVar(&reportProject, "project", "", "Filter to a specific project path")
	reportCmd.Flags().BoolVar(&reportJSON, "json", false, "Output as JSON")
	reportCmd.Flags().BoolVar(&reportAllTime, "all-time", false, "Report across entire log history")
}

// reportData is the structured output for --json.
type reportData struct {
	Window                string            `json:"window"`
	Compactions           int               `json:"compactions"`
	ContextBombsDelivered int               `json:"context_bombs_delivered"`
	SessionSnapshotsSaved int               `json:"session_snapshots_saved"`
	TotalContextBombBytes int               `json:"total_context_bomb_bytes"`
	TotalTokens           int               `json:"total_tokens"`
	TokensExact           bool              `json:"tokens_exact"`
	EstimatedHoursSaved   float64           `json:"estimated_hours_saved"`
	TopProjects           []projectActivity `json:"top_projects"`
	LastCompaction        *time.Time        `json:"last_compaction,omitempty"`
	LastCompactionProject string            `json:"last_compaction_project,omitempty"`
}

type projectActivity struct {
	Path        string `json:"path"`
	Compactions int    `json:"compactions"`
}

func reportHandler(cmd *cobra.Command, args []string) error {
	entries, err := activitylog.ReadAll()
	if err != nil {
		return fmt.Errorf("reading activity log: %w", err)
	}

	// Determine time window.
	var since time.Time
	var windowLabel string
	if reportAllTime {
		windowLabel = "all time"
	} else {
		since = time.Now().UTC().Truncate(24 * time.Hour).AddDate(0, 0, -reportDays)
		windowLabel = fmt.Sprintf("last %d days", reportDays)
	}

	// Normalize project filter to clean absolute path for comparison.
	var filterProject string
	if reportProject != "" {
		filterProject = filepath.Clean(reportProject)
	}

	filtered := filterEntries(entries, since, filterProject)
	rpt := buildReportData(filtered, windowLabel)

	// Replace the byte-based token estimate with exact counts from the SQLite injection log.
	// Apply the same time window and project filters as the activity log query.
	if dbPath, err := config.DBPath(); err == nil {
		if store, err := cache.Open(dbPath); err == nil {
			defer store.Close()
			var sincePtr *time.Time
			if !since.IsZero() {
				sincePtr = &since
			}
			var projectHash string
			if filterProject != "" {
				if info, err := project.Detect(context.Background(), filterProject); err == nil {
					projectHash = info.Hash
				}
			}
			if stats, err := store.GetStats(projectHash, sincePtr); err == nil && stats.TotalInjections > 0 {
				rpt.TotalTokens = stats.TotalTokens
				rpt.TokensExact = true
			}
		}
	}

	if reportJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(rpt)
	}

	return printReport(rpt)
}

// filterEntries returns the subset of entries within the time window and matching project.
// A zero since means no time filter (all-time mode). filterProject must already be filepath.Clean'd.
func filterEntries(entries []activitylog.Entry, since time.Time, filterProject string) []activitylog.Entry {
	var filtered []activitylog.Entry
	for _, e := range entries {
		if !since.IsZero() && e.Timestamp.Before(since) {
			continue
		}
		if filterProject != "" && filepath.Clean(e.Project) != filterProject {
			continue
		}
		filtered = append(filtered, e)
	}
	return filtered
}

// buildReportData aggregates a slice of filtered entries into a reportData struct.
func buildReportData(filtered []activitylog.Entry, windowLabel string) reportData {
	projectCounts := make(map[string]int)
	totalBytes := 0
	snapshots := 0
	var lastEntry *activitylog.Entry

	for i := range filtered {
		e := &filtered[i]
		projectCounts[e.Project]++
		totalBytes += e.ContextBombSizeBytes
		if e.SessionSnapshotPresent {
			snapshots++
		}
		if lastEntry == nil || e.Timestamp.After(lastEntry.Timestamp) {
			lastEntry = e
		}
	}

	// Build top-projects list sorted by count desc, then path asc.
	type kv struct {
		k string
		v int
	}
	kvs := make([]kv, 0, len(projectCounts))
	for k, v := range projectCounts {
		kvs = append(kvs, kv{k, v})
	}
	sort.Slice(kvs, func(i, j int) bool {
		if kvs[i].v != kvs[j].v {
			return kvs[i].v > kvs[j].v
		}
		return kvs[i].k < kvs[j].k
	})
	topN := 5
	if len(kvs) < topN {
		topN = len(kvs)
	}
	topProjects := make([]projectActivity, topN)
	for i := 0; i < topN; i++ {
		topProjects[i] = projectActivity{Path: kvs[i].k, Compactions: kvs[i].v}
	}

	// Fallback: each context bomb is ~4 bytes per token. Overridden by exact DB data in reportHandler.
	estimatedTokens := totalBytes / 4
	// Each compaction saved ~10 minutes of manual context recovery.
	estimatedHours := float64(len(filtered)) * 10.0 / 60.0

	var lastTime *time.Time
	var lastPath string
	if lastEntry != nil {
		t := lastEntry.Timestamp
		lastTime = &t
		lastPath = lastEntry.Project
	}

	return reportData{
		Window:                windowLabel,
		Compactions:           len(filtered),
		ContextBombsDelivered: len(filtered),
		SessionSnapshotsSaved: snapshots,
		TotalContextBombBytes: totalBytes,
		TotalTokens:           estimatedTokens,
		EstimatedHoursSaved:   estimatedHours,
		TopProjects:           topProjects,
		LastCompaction:        lastTime,
		LastCompactionProject: lastPath,
	}
}

func printReport(r reportData) error {
	fmt.Printf("\nUncompact Report — %s\n", r.Window)
	fmt.Println("════════════════════════════════")
	fmt.Println()
	fmt.Printf("  Compactions handled:     %d\n", r.Compactions)
	fmt.Printf("  Context bombs delivered: %d\n", r.ContextBombsDelivered)
	fmt.Printf("  Session snapshots saved: %d  (requires PreCompact hook)\n", r.SessionSnapshotsSaved)
	fmt.Println()
	if r.TokensExact {
		fmt.Printf("  Tokens restored:           %s\n", formatThousands(r.TotalTokens))
	} else {
		fmt.Printf("  Tokens restored (est.):    ~%s\n", formatThousands(r.TotalTokens))
	}
	fmt.Printf("  Estimated time saved:      ~%.1f hours\n", r.EstimatedHoursSaved)
	fmt.Println()

	if len(r.TopProjects) > 0 {
		fmt.Println("  Most active projects:")
		for _, p := range r.TopProjects {
			plural := "s"
			if p.Compactions == 1 {
				plural = ""
			}
			fmt.Printf("    • %s  (%d compaction%s)\n", p.Path, p.Compactions, plural)
		}
		fmt.Println()
	}

	if r.LastCompaction != nil {
		fmt.Printf("  Last compaction: %s in %s\n",
			r.LastCompaction.Local().Format("2006-01-02 at 15:04"),
			r.LastCompactionProject,
		)
		fmt.Println()
	}

	if r.Compactions == 0 {
		fmt.Println("  No compaction events recorded in this window.")
		fmt.Println("  Verify Uncompact is installed: uncompact install")
		fmt.Println()
	}

	fmt.Println("Run `uncompact report --json` for machine-readable output.")
	fmt.Println()
	return nil
}

// formatThousands formats an integer with comma separators (e.g. 42000 → "42,000").
func formatThousands(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%d,%03d,%03d", n/1_000_000, (n/1_000)%1_000, n%1_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%d,%03d", n/1_000, n%1_000)
	}
	return fmt.Sprintf("%d", n)
}
