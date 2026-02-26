package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/supermodeltools/uncompact/internal/db"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show last injection time, token size, and cache freshness",
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := db.Open("")
		if err != nil {
			return fmt.Errorf("could not open cache: %w", err)
		}
		defer store.Close()

		latest, err := store.GetLatest()
		if err != nil || latest == nil {
			fmt.Println("No cached context found. Run `uncompact run` to fetch context.")
			return nil
		}

		age := time.Since(latest.FetchedAt)
		stale := ""
		if age > latest.TTL {
			stale = " [STALE]"
		}

		fmt.Printf("Last fetch:   %s (%s ago)%s\n", latest.FetchedAt.Format(time.RFC3339), formatDuration(age), stale)
		fmt.Printf("Token size:   ~%d tokens\n", latest.EstimatedTokens)
		fmt.Printf("Cache TTL:    %s\n", latest.TTL)
		fmt.Printf("Source:       %s\n", latest.Source)
		return nil
	},
}

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show token usage, cache hit rate, and API call count",
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := db.Open("")
		if err != nil {
			return fmt.Errorf("could not open cache: %w", err)
		}
		defer store.Close()

		stats, err := store.GetStats()
		if err != nil {
			return fmt.Errorf("could not read stats: %w", err)
		}

		fmt.Printf("Total injections:  %d\n", stats.TotalInjections)
		fmt.Printf("Cache hits:        %d (%.1f%%)\n", stats.CacheHits, stats.CacheHitRate()*100)
		fmt.Printf("API calls:         %d\n", stats.APICalls)
		fmt.Printf("Avg token size:    %d\n", stats.AvgTokens)
		fmt.Printf("Total tokens used: %d\n", stats.TotalTokens)
		return nil
	},
}

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "View recent Uncompact activity",
	RunE: func(cmd *cobra.Command, args []string) error {
		tail, _ := cmd.Flags().GetInt("tail")
		store, err := db.Open("")
		if err != nil {
			return fmt.Errorf("could not open cache: %w", err)
		}
		defer store.Close()

		entries, err := store.GetLogs(tail)
		if err != nil {
			return fmt.Errorf("could not read logs: %w", err)
		}
		if len(entries) == 0 {
			fmt.Println("No log entries found.")
			return nil
		}
		for _, e := range entries {
			fmt.Printf("[%s] %s — %d tokens (%s)\n",
				e.Timestamp.Format("2006-01-02 15:04:05"), e.Event, e.Tokens, e.Source)
		}
		return nil
	},
}

var dryRunCmd = &cobra.Command{
	Use:   "dry-run",
	Short: "Show what would be injected without writing anything",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Delegate to run with dry-run flag behavior: print output but annotate it
		fmt.Println("--- DRY RUN: context bomb that would be injected ---")
		// Reuse run logic
		return runCmd.RunE(cmd, args)
	},
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh", int(d.Hours()))
}

func init() {
	logsCmd.Flags().Int("tail", 50, "number of recent log entries to show")
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(statsCmd)
	rootCmd.AddCommand(logsCmd)
	rootCmd.AddCommand(dryRunCmd)
}
