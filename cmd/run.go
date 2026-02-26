package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/supermodeltools/uncompact/internal/api"
	"github.com/supermodeltools/uncompact/internal/cache"
	"github.com/supermodeltools/uncompact/internal/config"
	"github.com/supermodeltools/uncompact/internal/project"
	tmpl "github.com/supermodeltools/uncompact/internal/template"
	"github.com/supermodeltools/uncompact/internal/zip"
)

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

	// Detect project
	proj, err := project.Detect("")
	if err != nil {
		logFn("[warn] project detection failed: %v", err)
		return silentExit()
	}
	logFn("[debug] project: %s (hash: %s)", proj.Name, proj.Hash)

	// Open cache
	dbPath, err := config.DBPath()
	if err != nil {
		logFn("[warn] cannot open cache: %v", err)
		return runWithoutCache(cfg, proj, logFn)
	}

	store, err := cache.Open(dbPath)
	if err != nil {
		logFn("[warn] cache open error: %v", err)
		return runWithoutCache(cfg, proj, logFn)
	}
	defer store.Close()

	// Background prune on an isolated DB handle to avoid racing with store.Close().
	go func(path string) {
		pruneStore, err := cache.Open(path)
		if err != nil {
			return
		}
		defer pruneStore.Close()
		_ = pruneStore.Prune()
	}(dbPath)

	// Check cache
	var graph *api.ProjectGraph
	var source string
	var stale bool
	var staleAt *time.Time

	if !forceRefresh {
		cached, fresh, expiresAt, err := store.Get(proj.Hash)
		if err != nil {
			logFn("[warn] cache read error: %v", err)
		} else if cached != nil {
			graph = cached
			if fresh {
				source = "cache"
				logFn("[debug] serving fresh cached graph")
			} else {
				stale = true
				staleAt = expiresAt // when the cache entry expired
				source = "stale_cache"
				logFn("[debug] serving stale cached graph (will refresh in background if API available)")
			}
		}
	}

	// If no cache or forced refresh, fetch from API
	if graph == nil || forceRefresh {
		logFn("[debug] fetching from Supermodel API...")
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		zipData, err := zip.RepoZip(proj.RootDir)
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
			apiClient := api.New(cfg.BaseURL, cfg.APIKey, debug, logFn)
			freshGraph, err := apiClient.GetGraph(ctx, proj.Name, zipData)
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
		MaxTokens: maxTokens,
		Stale:     stale,
		StaleAt:   staleAt,
	}
	output, tokens, err := tmpl.Render(graph, proj.Name, opts)
	if err != nil {
		logFn("[warn] render error: %v", err)
		return silentExit()
	}

	// Emit context bomb to stdout
	fmt.Print(output)

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
func runWithoutCache(cfg *config.Config, proj *project.Info, logFn func(string, ...interface{})) error {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	zipData, err := zip.RepoZip(proj.RootDir)
	if err != nil {
		logFn("[warn] zip error: %v", err)
		if fallback {
			printFallback(proj.Name)
		}
		return silentExit()
	}

	apiClient := api.New(cfg.BaseURL, cfg.APIKey, debug, logFn)
	graph, err := apiClient.GetGraph(ctx, proj.Name, zipData)
	if err != nil {
		logFn("[warn] API error: %v", err)
		if fallback {
			printFallback(proj.Name)
		}
		return silentExit()
	}

	opts := tmpl.RenderOptions{MaxTokens: maxTokens}
	output, _, err := tmpl.Render(graph, proj.Name, opts)
	if err != nil {
		logFn("[warn] render error: %v", err)
		if fallback {
			printFallback(proj.Name)
		}
		return silentExit()
	}

	fmt.Print(output)
	return nil
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

// makeLogger returns a logging function that writes to stderr if debug is enabled.
func makeLogger() func(string, ...interface{}) {
	if debug {
		return func(format string, args ...interface{}) {
			fmt.Fprintf(os.Stderr, format+"\n", args...)
		}
	}
	return func(string, ...interface{}) {}
}
