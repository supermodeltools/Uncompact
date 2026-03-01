package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/supermodeltools/uncompact/internal/api"
	"github.com/supermodeltools/uncompact/internal/cache"
	"github.com/supermodeltools/uncompact/internal/config"
	"github.com/supermodeltools/uncompact/internal/hooks"
	"github.com/supermodeltools/uncompact/internal/local"
	"github.com/supermodeltools/uncompact/internal/project"
	tmpl "github.com/supermodeltools/uncompact/internal/template"
	"github.com/supermodeltools/uncompact/internal/zip"
)

var logsLimit int
var logsProjectFlag bool
var statsProjectFlag bool

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show last injection time, cache state, and auth status",
	RunE:  statusHandler,
}

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Show recent injection activity",
	RunE:  logsHandler,
}

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show token usage and cache statistics",
	RunE:  statsHandler,
}

var dryRunCmd = &cobra.Command{
	Use:   "dry-run",
	Short: "Preview the context bomb without emitting it",
	Long:  `dry-run shows what would be injected. Metadata is written to stderr; the context bomb itself is written to stdout. Useful for debugging.`,
	RunE:  dryRunHandler,
}

var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Manage the local SQLite cache",
}

var cacheClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Clear all cached graph data",
	RunE:  cacheClearHandler,
}

var cacheProjectFlag bool

func init() {
	logsCmd.Flags().IntVar(&logsLimit, "tail", 20, "Number of recent log entries to show")
	logsCmd.Flags().BoolVar(&logsProjectFlag, "project", false, "Show logs for the current project only")
	statsCmd.Flags().BoolVar(&statsProjectFlag, "project", false, "Show stats for the current project only")
	cacheCmd.AddCommand(cacheClearCmd)
	cacheClearCmd.Flags().BoolVar(&cacheProjectFlag, "project", false, "Clear only the current project's cache")
	rootCmd.AddCommand(statusCmd, logsCmd, statsCmd, dryRunCmd, cacheCmd)
}

func statusHandler(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(apiKey)
	if err != nil {
		return err
	}

	// Auth status
	if cfg.IsAuthenticated() {
		fmt.Printf("Auth:     authenticated (key: %s...)\n",
			cfg.APIKey[:min(4, len(cfg.APIKey))])
	} else {
		fmt.Println("Auth:     NOT authenticated (run 'uncompact auth login')")
	}

	// Hooks status
	settingsPath, _ := hooks.FindSettingsFile()
	if settingsPath != "" {
		installed, err := hooks.Verify(settingsPath)
		if err != nil {
			fmt.Printf("Hooks:    error checking (%v)\n", err)
		} else if installed {
			fmt.Printf("Hooks:    installed (%s)\n", settingsPath)
		} else {
			fmt.Printf("Hooks:    NOT installed (run 'uncompact install')\n")
		}
	}

	// Cache / last injection
	gitCtx, gitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer gitCancel()
	proj, err := project.Detect(gitCtx, "")
	if err != nil {
		fmt.Printf("Project:  unknown (%v)\n", err)
		return nil
	}
	fmt.Printf("Project:  %s (hash: %s)\n", proj.Name, proj.Hash)

	dbPath, err := config.DBPath()
	if err != nil {
		fmt.Printf("Cache:    unavailable (%v)\n", err)
		return nil
	}

	store, err := cache.Open(dbPath)
	if err != nil {
		fmt.Printf("Cache:    error (%v)\n", err)
		return nil
	}
	defer store.Close()

	// Last injection
	last, err := store.LastInjection(proj.Hash)
	if err != nil || last == nil {
		fmt.Println("Last injection: never")
	} else {
		age := time.Since(last.CreatedAt)
		staleStr := ""
		if last.StaleAt != nil {
			staleStr = " [STALE]"
		}
		fmt.Printf("Last injection: %s ago (%d tokens, source: %s%s)\n",
			humanDuration(age), last.Tokens, last.Source, staleStr)
	}

	// Cache freshness
	graph, fresh, expiresAt, _, err := store.Get(proj.Hash)
	if err != nil {
		fmt.Printf("Cache:    error (%v)\n", err)
		return nil
	}
	fmt.Printf("Cache:    %s\n", formatCacheStatus(graph != nil, fresh, expiresAt, time.Now()))

	return nil
}

func logsHandler(cmd *cobra.Command, args []string) error {
	dbPath, err := config.DBPath()
	if err != nil {
		return err
	}
	store, err := cache.Open(dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	var projectHash string
	if logsProjectFlag {
		gitCtx, gitCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer gitCancel()
		proj, err := project.Detect(gitCtx, "")
		if err != nil {
			return fmt.Errorf("project detection failed: %w", err)
		}
		projectHash = proj.Hash
		fmt.Printf("Showing logs for project: %s\n\n", proj.Name)
	}

	logs, err := store.RecentLogs(logsLimit, projectHash)
	if err != nil {
		return err
	}

	if len(logs) == 0 {
		fmt.Println("No injection logs found.")
		return nil
	}

	fmt.Printf("%-20s %-20s %6s  %-12s  %s\n", "TIME", "PROJECT", "TOKENS", "SOURCE", "FLAGS")
	fmt.Println(strings.Repeat("-", 75))
	for _, l := range logs {
		flags := ""
		if l.StaleAt != nil {
			flags = "STALE"
		}
		fmt.Printf("%-20s %-20s %6d  %-12s  %s\n",
			l.CreatedAt.Format("01/02 15:04:05"),
			truncate(l.ProjectName, 20),
			l.Tokens,
			l.Source,
			flags,
		)
	}
	return nil
}

func statsHandler(cmd *cobra.Command, args []string) error {
	dbPath, err := config.DBPath()
	if err != nil {
		return err
	}
	store, err := cache.Open(dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	var projectHash string
	if statsProjectFlag {
		gitCtx, gitCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer gitCancel()
		proj, err := project.Detect(gitCtx, "")
		if err != nil {
			return fmt.Errorf("project detection failed: %w", err)
		}
		projectHash = proj.Hash
		fmt.Printf("Showing stats for project: %s\n\n", proj.Name)
	}

	st, err := store.GetStats(projectHash, nil)
	if err != nil {
		return err
	}

	fmt.Printf("Total injections:  %d\n", st.TotalInjections)
	fmt.Printf("API fetches:       %d\n", st.APIFetches)
	fmt.Printf("Fresh cache hits:  %d\n", st.FreshCacheHits)
	fmt.Printf("Stale cache hits:  %d\n", st.StaleCacheHits)
	fmt.Printf("Local builds:      %d\n", st.LocalBuilds)
	totalCacheHits := st.FreshCacheHits + st.StaleCacheHits
	if st.TotalInjections > 0 {
		hitRate := float64(totalCacheHits) / float64(st.TotalInjections) * 100
		fmt.Printf("Cache hit rate:    %.1f%%\n", hitRate)
	}
	if totalCacheHits > 0 {
		staleRate := float64(st.StaleCacheHits) / float64(totalCacheHits) * 100
		fmt.Printf("Stale hit rate:    %.1f%%\n", staleRate)
	}
	fmt.Printf("Total tokens:      %d\n", st.TotalTokens)
	if st.AvgTokens > 0 {
		fmt.Printf("Avg tokens/bomb:   %.0f\n", st.AvgTokens)
	}
	return nil
}

func dryRunHandler(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(apiKey)
	if err != nil {
		return err
	}

	effectiveMode, err := cfg.EffectiveMode(mode)
	if err != nil {
		return err
	}
	if effectiveMode == config.ModeLocal {
		return dryRunLocalMode()
	}

	gitCtx, gitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer gitCancel()
	proj, err := project.Detect(gitCtx, "")
	if err != nil {
		return fmt.Errorf("project detection failed: %w", err)
	}

	logFn := makeLogger()

	// Gather working memory from git and GitHub (failures are silent).
	// Use a longer timeout for the gh CLI call, which is a network operation.
	wmCtx, wmCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer wmCancel()
	wm := project.GetWorkingMemory(wmCtx, proj.RootDir, logFn)

	dbPath, err := config.DBPath()
	if err != nil {
		return err
	}
	store, err := cache.Open(dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	claudeMD := local.ReadClaudeMD(proj.RootDir)

	// Try cache first
	cachedGraph, fresh, _, fetchedAt, err := store.Get(proj.Hash)
	if err != nil {
		return fmt.Errorf("reading cache: %w", err)
	}

	if cachedGraph != nil && !forceRefresh {
		if !fresh {
			fmt.Fprintln(os.Stderr, "[dry-run] WARNING: serving stale cache")
		} else {
			fmt.Fprintln(os.Stderr, "[dry-run] serving cached graph")
		}
		opts := tmpl.RenderOptions{
			MaxTokens:     maxTokens,
			Stale:         !fresh,
			StaleAt:       fetchedAt,
			WorkingMemory: wm,
			ClaudeMD:      claudeMD,
		}
		output, tokens, err := tmpl.Render(cachedGraph, proj.Name, opts)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "[dry-run] %d tokens (max: %d)\n", tokens, maxTokens)
		fmt.Fprintln(os.Stderr, "--- context bomb preview ---")
		fmt.Print(output)
		return nil
	}

	if !cfg.IsAuthenticated() {
		return fmt.Errorf("not authenticated — run 'uncompact auth login'")
	}

	// No cached graph — fetch from API but don't persist anything.
	fmt.Fprintln(os.Stderr, "[dry-run] no cache — fetching from API (results will NOT be cached)")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	zipData, skipReport, err := zip.RepoZip(ctx, proj.RootDir)
	if err != nil {
		return fmt.Errorf("zip error: %w", err)
	}
	logZipSkips(skipReport)

	apiClient := api.New(cfg.BaseURL, cfg.APIKey, debug, logFn)
	graph, err := fetchGraphWithCircularDeps(ctx, apiClient, proj.Name, zipData)
	if err != nil {
		return fmt.Errorf("API error: %w", err)
	}

	opts := tmpl.RenderOptions{
		MaxTokens:     maxTokens,
		WorkingMemory: wm,
		ClaudeMD:      claudeMD,
	}
	output, tokens, err := tmpl.Render(graph, proj.Name, opts)
	if err != nil {
		return fmt.Errorf("render error: %w", err)
	}

	fmt.Fprintf(os.Stderr, "[dry-run] %d tokens (max: %d)\n", tokens, maxTokens)
	fmt.Fprintln(os.Stderr, "--- context bomb preview ---")
	fmt.Print(output)
	return nil
}

// dryRunLocalMode runs dry-run using local repository analysis only,
// requiring no API key. On cache miss it calls local.BuildProjectGraph;
// results are NOT written back to cache (consistent with API dry-run behaviour).
func dryRunLocalMode() error {
	fmt.Fprintln(os.Stderr, "[dry-run] local mode — no API key required")

	gitCtx, gitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer gitCancel()
	proj, err := project.Detect(gitCtx, "")
	if err != nil {
		return fmt.Errorf("project detection failed: %w", err)
	}

	logFn := makeLogger()

	wmCtx, wmCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer wmCancel()
	wm := project.GetWorkingMemory(wmCtx, proj.RootDir, logFn)

	dbPath, err := config.DBPath()
	if err != nil {
		return err
	}
	store, err := cache.Open(dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	var graph *api.ProjectGraph

	// Try fresh cache first to avoid rebuilding on every dry-run.
	if !forceRefresh {
		cached, fresh, _, _, err := store.Get(proj.Hash)
		if err != nil {
			return fmt.Errorf("reading cache: %w", err)
		}
		if cached != nil && fresh {
			graph = cached
			fmt.Fprintln(os.Stderr, "[dry-run] serving cached local graph")
		}
	}

	// Build from local analysis on cache miss — results are NOT cached.
	if graph == nil {
		fmt.Fprintln(os.Stderr, "[dry-run] no cache — building from local analysis (results will NOT be cached)")
		localCtx, localCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer localCancel()

		built, err := local.BuildProjectGraph(localCtx, proj.RootDir, proj.Name)
		if err != nil {
			return fmt.Errorf("local graph build failed: %w", err)
		}
		graph = built
	}

	claudeMD := local.ReadClaudeMD(proj.RootDir)
	opts := tmpl.RenderOptions{
		MaxTokens:     maxTokens,
		WorkingMemory: wm,
		ClaudeMD:      claudeMD,
		LocalMode:     true,
	}
	output, tokens, err := tmpl.Render(graph, proj.Name, opts)
	if err != nil {
		return fmt.Errorf("render error: %w", err)
	}

	fmt.Fprintf(os.Stderr, "[dry-run] %d tokens (max: %d)\n", tokens, maxTokens)
	fmt.Fprintln(os.Stderr, "--- context bomb preview ---")
	fmt.Print(output)
	return nil
}

func cacheClearHandler(cmd *cobra.Command, args []string) error {
	dbPath, err := config.DBPath()
	if err != nil {
		return err
	}
	store, err := cache.Open(dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	if cacheProjectFlag {
		gitCtx, gitCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer gitCancel()
		proj, err := project.Detect(gitCtx, "")
		if err != nil {
			return fmt.Errorf("project detection failed: %w", err)
		}
		if err := store.ClearProject(proj.Hash); err != nil {
			return err
		}
		fmt.Printf("Cache cleared for project: %s\n", proj.Name)
	} else {
		if err := store.ClearAll(); err != nil {
			return err
		}
		fmt.Println("All cache entries cleared.")
	}
	return nil
}

func truncate(s string, n int) string {
	if n <= 3 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-3]) + "..."
}

func formatCacheStatus(hasGraph bool, fresh bool, expiresAt *time.Time, now time.Time) string {
	if !hasGraph {
		return "empty"
	}
	if fresh {
		if expiresAt != nil {
			return fmt.Sprintf("fresh (expires in %s)", humanDuration(expiresAt.Sub(now)))
		}
		return "fresh"
	}
	if expiresAt != nil {
		return fmt.Sprintf("stale (expired %s ago, will refresh on next run)", humanDuration(now.Sub(*expiresAt)))
	}
	return "stale (will refresh on next run)"
}

func humanDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%.1fh", d.Hours())
	}
	return fmt.Sprintf("%.1fd", d.Hours()/24)
}
