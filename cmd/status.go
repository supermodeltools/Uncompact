package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/supermodeltools/uncompact/internal/config"
	"github.com/supermodeltools/uncompact/internal/db"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show last injection time, cache state, and token size",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			fmt.Println("Auth:      not configured (run: uncompact auth login)")
		} else if cfg.APIKey == "" {
			fmt.Println("Auth:      no API key set (run: uncompact auth login)")
		} else {
			fmt.Println("Auth:      ✓ configured")
		}

		if cfg == nil {
			cfg = config.Default()
		}

		store, err := db.Open(cfg.DBPath)
		if err != nil {
			fmt.Printf("Cache:     unavailable (%v)\n", err)
			return nil
		}
		defer store.Close()

		hash := workspaceHash()
		wd, _ := os.Getwd()
		fmt.Printf("Workspace: %s\n", wd)

		last, err := store.LastInjectionTime(hash)
		if err != nil || last.IsZero() {
			fmt.Println("Last injection: never")
		} else {
			fmt.Printf("Last injection: %s (%v ago)\n",
				last.Format(time.RFC3339), time.Since(last).Round(time.Second))
		}

		cached, _ := store.GetStaleCachedGraph(hash)
		if cached == nil {
			fmt.Println("Cache:     empty")
		} else {
			age := time.Since(cached.FetchedAt)
			expired := ""
			if cached.IsExpired() {
				expired = " [STALE]"
			}
			fmt.Printf("Cache:     present (age: %v%s)\n", age.Round(time.Second), expired)
		}

		return nil
	},
}

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "View recent injection activity",
	RunE: func(cmd *cobra.Command, args []string) error {
		tail, _ := cmd.Flags().GetInt("tail")
		cfg, err := config.Load()
		if err != nil {
			cfg = config.Default()
		}

		store, err := db.Open(cfg.DBPath)
		if err != nil {
			return fmt.Errorf("failed to open cache: %w", err)
		}
		defer store.Close()

		entries, err := store.GetLogs(tail)
		if err != nil {
			return fmt.Errorf("failed to read logs: %w", err)
		}

		if len(entries) == 0 {
			fmt.Println("No injection logs found.")
			return nil
		}

		fmt.Printf("%-25s %-12s %-10s %s\n", "TIME", "TOKENS", "CACHE HIT", "WORKSPACE")
		fmt.Println("-------------------------------------------------------------------")
		for _, e := range entries {
			cacheStr := "no"
			if e.CacheHit {
				cacheStr = "yes"
			}
			fmt.Printf("%-25s %-12d %-10s %s\n",
				e.InjectedAt.Format(time.RFC3339),
				e.TokensInjected,
				cacheStr,
				e.WorkspaceHash[:8]+"...",
			)
		}
		return nil
	},
}

var dryRunCmd = &cobra.Command{
	Use:   "dry-run",
	Short: "Show what context bomb would be injected without outputting it",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Redirect output to stderr summary instead of stdout
		fmt.Fprintln(os.Stderr, "[dry-run] Simulating context injection...")
		fmt.Fprintln(os.Stderr, "[dry-run] Workspace:", func() string { wd, _ := os.Getwd(); return wd }())
		fmt.Fprintln(os.Stderr, "[dry-run] Max tokens:", maxTokens)
		fmt.Fprintln(os.Stderr, "[dry-run] Force refresh:", forceRefresh)

		// Temporarily capture stdout output from run
		oldFallback := fallbackMode
		fallbackMode = true
		defer func() { fallbackMode = oldFallback }()

		// Run but write to stderr
		cfg, _ := config.Load()
		if cfg == nil {
			cfg = config.Default()
		}

		fmt.Fprintln(os.Stderr, "[dry-run] Auth configured:", cfg.APIKey != "")
		fmt.Fprintln(os.Stderr, "[dry-run] Run 'uncompact run' to execute injection")
		return nil
	},
}

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show token usage, cache hit rate, and API call counts",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			cfg = config.Default()
		}

		store, err := db.Open(cfg.DBPath)
		if err != nil {
			return fmt.Errorf("failed to open cache: %w", err)
		}
		defer store.Close()

		stats, err := store.GetStats()
		if err != nil {
			return fmt.Errorf("failed to read stats: %w", err)
		}

		fmt.Printf("Total injections:  %d\n", stats.TotalInjections)
		fmt.Printf("Total tokens:      %d\n", stats.TotalTokens)
		fmt.Printf("Cache hits:        %d (%.1f%%)\n",
			stats.CacheHits, percent(stats.CacheHits, stats.TotalInjections))
		fmt.Printf("API calls:         %d\n", stats.APICalls)
		if stats.TotalInjections > 0 {
			fmt.Printf("Avg tokens/inject: %d\n", stats.TotalTokens/stats.TotalInjections)
		}
		return nil
	},
}

func percent(part, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total) * 100
}

func init() {
	logsCmd.Flags().Int("tail", 20, "Number of recent log entries to show")
}
