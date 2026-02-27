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
	"github.com/supermodeltools/uncompact/internal/project"
	tmpl "github.com/supermodeltools/uncompact/internal/template"
	"github.com/supermodeltools/uncompact/internal/zip"
)

var postCompact bool
var maxStale time.Duration

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Emit a context bomb to stdout (used by the Claude Code hook)",
	Long: `Run generates a context bomb from the current project and writes it to stdout.
This is the command invoked by the Claude Code Stop hook after compaction.

On failure, it exits cleanly with no output to avoid disrupting the session.`,
	RunE: runHandler,
	// Don't show usage on error — this is a hook command
	SilenceErrors: true,
	SilenceUsage:  true,
}

func init() {
	rootCmd.AddCommand(runCmd)
	runCmd.Flags().BoolVar(&postCompact, "post-compact", false, "Append acknowledgment instruction so Claude confirms context restoration in its response")
	runCmd.Flags().DurationVar(&maxStale, "max-stale", 24*time.Hour, "Maximum age of stale cache to serve when API is unavailable (0 = no limit)")
}

func runHandler(cmd *cobra.Command, args []string) error {
	logFn := makeLogger()

	// Load config
	cfg, err := config.Load(apiKey)
	if err != nil {
		logFn("[warn] config error: %v", err)
		return silentExit()
	}

	if !cfg.IsAuthenticated() {
		logFn("[warn] no API key configured — run 'uncompact auth login' to authenticate")
		if fallback {
			printFallback("(no API key configured)")
		}
		return silentExit()
	}

	// Detect project and gather working memory with a short timeout so slow
	// or broken git/gh operations never hang the hook indefinitely.
	gitCtx, gitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer gitCancel()

	proj, err := project.Detect(gitCtx, "")
	if err != nil {
		logFn("[warn] project detection failed: %v", err)
		return silentExit()
	}
	logFn("[debug] project: %s (hash: %s)", proj.Name, proj.Hash)

	// Gather working memory from git and GitHub (failures are silent).
	// Use a longer timeout for the gh CLI call, which is a network operation.
	wmCtx, wmCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer wmCancel()
	wm := project.GetWorkingMemory(wmCtx, proj.RootDir)

	// Open cache
	dbPath, err := config.DBPath()
	if err != nil {
		logFn("[warn] cannot open cache: %v", err)
		return runWithoutCache(cfg, proj, wm, postCompact, logFn)
	}

	store, err := cache.Open(dbPath)
	if err != nil {
		logFn("[warn] cache open error: %v", err)
		return runWithoutCache(cfg, proj, wm, postCompact, logFn)
	}
	defer store.Close()

	// Background prune on an isolated DB handle to avoid racing with store.Close().
	go func(path string) {
		pruneStore, err := cache.Open(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[warn] cache prune: failed to open store: %v\n", err)
			return
		}
		defer pruneStore.Close()
		if err := pruneStore.Prune(); err != nil {
			fmt.Fprintf(os.Stderr, "[warn] cache prune: %v\n", err)
		}
	}(dbPath)

	// Check cache
	var graph *api.ProjectGraph
	var source string
	var stale bool
	var staleAt *time.Time

	if !forceRefresh {
		cached, fresh, _, fetchedAt, err := store.Get(proj.Hash)
		if err != nil {
			logFn("[warn] cache read error: %v", err)
		} else if cached != nil {
			graph = cached
			if fresh {
				source = "cache"
				logFn("[debug] serving fresh cached graph")
			} else {
				stale = true
				staleAt = fetchedAt // when the data was originally fetched
				source = "stale_cache"
				logFn("[debug] serving stale cached graph (will refresh in background if API available)")

				// Enforce max-stale: if the cached data is older than allowed, discard it.
				if maxStale > 0 && fetchedAt != nil && time.Since(*fetchedAt) > maxStale {
					age := time.Since(*fetchedAt).Round(time.Minute)
					logFn("[warn] stale cache too old (fetched %v ago, max-stale %v) — treating as cache miss", age, maxStale)
					graph = nil
					stale = false
					staleAt = nil
				}
			}
		}
	}

	// If no cache or forced refresh, fetch from API
	if graph == nil || forceRefresh {
		logFn("[debug] fetching from Supermodel API...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		zipData, skipReport, err := zip.RepoZip(proj.RootDir)
		if err != nil {
			logFn("[warn] zip error: %v", err)
			if !stale || graph == nil {
				if fallback {
					printFallback(proj.Name)
				}
				return silentExit()
			}
			// else: fall through to use stale cache
		} else {
			logZipSkips(skipReport)
			apiClient := api.New(cfg.BaseURL, cfg.APIKey, debug, logFn)
			freshGraph, err := fetchGraphWithCircularDeps(ctx, apiClient, proj.Name, zipData)
			if err != nil {
				logFn("[warn] API error: %v", err)
				if graph == nil {
					if fallback {
						printFallback(proj.Name)
					}
					return silentExit()
				}
				// Fall through to use stale cache
			} else {
				graph = freshGraph
				source = "api"
				stale = false
				staleAt = nil
				// Cache the fresh result
				if storeErr := store.Set(proj.Hash, proj.Name, graph); storeErr != nil {
					logFn("[warn] cache write error: %v", storeErr)
				}
			}
		}
	}

	if graph == nil {
		if fallback {
			printFallback(proj.Name)
		}
		return silentExit()
	}

	// Render context bomb
	opts := tmpl.RenderOptions{
		MaxTokens:     maxTokens,
		Stale:         stale,
		StaleAt:       staleAt,
		WorkingMemory: wm,
		PostCompact:   postCompact,
	}
	output, tokens, err := tmpl.Render(graph, proj.Name, opts)
	if err != nil {
		logFn("[warn] render error: %v", err)
		return silentExit()
	}

	// Emit context bomb to stdout
	fmt.Print(output)

	// Write to display cache so the UserPromptSubmit hook (show-cache) can display it.
	if err := writeDisplayCache(output); err != nil {
		logFn("[warn] display cache write error: %v", err)
	}

	// Log the injection
	var staleLogTime *time.Time
	if stale {
		staleLogTime = staleAt
	}
	_ = store.LogInjection(proj.Hash, proj.Name, tokens, source, staleLogTime)

	logFn("[debug] context bomb emitted: %d tokens, source: %s", tokens, source)
	return nil
}

// runWithoutCache attempts an API fetch with no cache fallback.
func runWithoutCache(cfg *config.Config, proj *project.Info, wm *project.WorkingMemory, postCompact bool, logFn func(string, ...interface{})) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	zipData, skipReport, err := zip.RepoZip(proj.RootDir)
	if err != nil {
		logFn("[warn] zip error: %v", err)
		if fallback {
			printFallback(proj.Name)
		}
		return silentExit()
	}
	logZipSkips(skipReport)

	apiClient := api.New(cfg.BaseURL, cfg.APIKey, debug, logFn)
	graph, err := fetchGraphWithCircularDeps(ctx, apiClient, proj.Name, zipData)
	if err != nil {
		logFn("[warn] API error: %v", err)
		if fallback {
			printFallback(proj.Name)
		}
		return silentExit()
	}

	opts := tmpl.RenderOptions{MaxTokens: maxTokens, WorkingMemory: wm, PostCompact: postCompact}
	output, _, err := tmpl.Render(graph, proj.Name, opts)
	if err != nil {
		logFn("[warn] render error: %v", err)
		if fallback {
			printFallback(proj.Name)
		}
		return silentExit()
	}

	fmt.Print(output)

	// Write to display cache so the UserPromptSubmit hook (show-cache) can display it.
	_ = writeDisplayCache(output)

	return nil
}

// fetchGraphWithCircularDeps fetches the project graph and circular dependency
// analysis via a single multipart upload. It delegates to GetGraphAndCircularDeps
// which builds the request body once and runs both analyses concurrently.
func fetchGraphWithCircularDeps(
	ctx context.Context,
	client *api.Client,
	projectName string,
	repoZip []byte,
) (*api.ProjectGraph, error) {
	return client.GetGraphAndCircularDeps(ctx, projectName, repoZip)
}

// silentExit returns nil (success) so we never block Claude Code sessions.
func silentExit() error {
	return nil
}

// printFallback emits a minimal static context bomb when the full one isn't available.
func printFallback(projectName string) {
	if projectName == "" {
		projectName = "Unknown Project"
	}
	fmt.Printf("# Uncompact Context — %s\n\n> Context unavailable (API or cache error). Run `uncompact run --debug` to diagnose.\n",
		projectName)
}

// logZipSkips prints diagnostic warnings for any files excluded from the zip.
func logZipSkips(report zip.SkipReport) {
	if len(report.OversizedFiles) > 0 {
		examples := report.OversizedFiles
		if len(examples) > 3 {
			examples = examples[:3]
		}
		names := strings.Join(examples, ", ")
		if len(report.OversizedFiles) > 3 {
			names += ", ..."
		}
		fmt.Fprintf(os.Stderr, "[warn] zip: skipped %d file(s) over 512KB (%s)\n",
			len(report.OversizedFiles), names)
	}
	if report.BudgetSkipped > 0 {
		fmt.Fprintf(os.Stderr, "[warn] zip truncated: %d additional file(s) excluded — total exceeded 10MB\n",
			report.BudgetSkipped)
	}
}

// makeLogger returns a logging function that writes to stderr if debug is enabled.
func makeLogger() func(string, ...interface{}) {
	if debug {
		return func(format string, args ...interface{}) {
			fmt.Fprintf(os.Stderr, format+"\n", args...)
		}
	}
	return func(string, ...interface{}) {}
}
