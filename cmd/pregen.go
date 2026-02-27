package cmd

import (
	"context"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/supermodeltools/uncompact/internal/api"
	"github.com/supermodeltools/uncompact/internal/cache"
	"github.com/supermodeltools/uncompact/internal/config"
	"github.com/supermodeltools/uncompact/internal/local"
	"github.com/supermodeltools/uncompact/internal/project"
	"github.com/supermodeltools/uncompact/internal/zip"
)

var pregenCmd = &cobra.Command{
	Use:   "pregen",
	Short: "Pre-generate and cache the project graph in the background",
	Long: `Pregen fetches the project graph from the Supermodel API and caches it silently.

Run before compaction occurs so the Stop hook can serve the context bomb instantly
from cache rather than waiting 10-15 minutes for a fresh API response.

If the cache is already fresh, pregen exits immediately without making any API call.
Safe to run from hooks — writes nothing to stdout.`,
	RunE:          pregenHandler,
	SilenceErrors: true,
	SilenceUsage:  true,
}

func init() {
	rootCmd.AddCommand(pregenCmd)
}

func pregenHandler(cmd *cobra.Command, args []string) error {
	logFn := makeLogger()

	// Load config
	cfg, err := config.Load(apiKey)
	if err != nil {
		logFn("[warn] config error: %v", err)
		return nil // silent exit — never block hooks
	}

	effectiveMode := cfg.EffectiveMode(mode)
	if effectiveMode == config.ModeLocal {
		return pregenLocalMode(logFn)
	}

	if !cfg.IsAuthenticated() {
		logFn("[warn] no API key configured — skipping pregen")
		return nil
	}

	// Detect project with a short timeout so slow or broken git operations
	// never hang the hook indefinitely.
	gitCtx, gitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer gitCancel()

	proj, err := project.Detect(gitCtx, "")
	if err != nil {
		logFn("[warn] project detection failed: %v", err)
		return nil
	}
	logFn("[debug] pregen for project: %s (hash: %s)", proj.Name, proj.Hash)

	// Open cache
	dbPath, err := config.DBPath()
	if err != nil {
		logFn("[warn] cannot open cache, skipping pregen: %v", err)
		return nil
	}

	store, err := cache.Open(dbPath)
	if err != nil {
		logFn("[warn] cache open error, skipping pregen: %v", err)
		return nil
	}
	defer store.Close()

	// Check if cache is already fresh — skip API call if so
	if !forceRefresh {
		_, fresh, _, _, err := store.Get(proj.Hash)
		if err == nil && fresh {
			logFn("[debug] cache is fresh, skipping pregen")
			return nil
		}
	}

	// Acquire an exclusive file lock so that only one pregen instance makes the
	// API call at a time. If another instance already holds the lock (i.e. it is
	// mid-flight on the same long-poll job), exit silently rather than firing a
	// redundant request.
	lockPath := filepath.Join(filepath.Dir(dbPath), "pregen.lock")
	unlock, acquired, err := acquirePregenLock(lockPath)
	if err != nil {
		logFn("[warn] lock error: %v", err)
		return nil
	}
	if !acquired {
		logFn("[debug] another pregen is already running, exiting silently")
		return nil
	}
	defer unlock()

	// Re-check freshness now that we hold the lock — a racing pregen instance may
	// have populated the cache while we were waiting for the lock to be released.
	if !forceRefresh {
		_, fresh, _, _, err := store.Get(proj.Hash)
		if err == nil && fresh {
			logFn("[debug] cache populated by concurrent pregen, skipping API call")
			return nil
		}
	}

	// Fetch from API with extended timeout (runs in background, so waiting is fine)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	logFn("[debug] fetching project graph from Supermodel API...")

	zipData, skipReport, err := zip.RepoZip(proj.RootDir)
	if err != nil {
		logFn("[warn] zip error: %v", err)
		return nil
	}
	logZipSkips(skipReport)

	apiClient := api.New(cfg.BaseURL, cfg.APIKey, debug, logFn)
	graph, err := fetchGraphWithCircularDeps(ctx, apiClient, proj.Name, zipData)
	if err != nil {
		logFn("[warn] API error: %v", err)
		return nil
	}

	if err := store.Set(proj.Hash, proj.Name, graph); err != nil {
		logFn("[warn] cache write error: %v", err)
		return nil
	}

	logFn("[debug] pregen complete: graph cached for %s", proj.Name)
	return nil
}

// pregenLocalMode builds and caches the project graph using local repository
// analysis only, with no API call required. Mirrors the API-mode flow so that
// subsequent `run` invocations can serve the cached result instantly.
func pregenLocalMode(logFn func(string, ...interface{})) error {
	gitCtx, gitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer gitCancel()

	proj, err := project.Detect(gitCtx, "")
	if err != nil {
		logFn("[warn] project detection failed: %v", err)
		return nil
	}
	logFn("[debug] local pregen for project: %s (hash: %s)", proj.Name, proj.Hash)

	dbPath, err := config.DBPath()
	if err != nil {
		logFn("[warn] cannot open cache, skipping pregen: %v", err)
		return nil
	}

	store, err := cache.Open(dbPath)
	if err != nil {
		logFn("[warn] cache open error, skipping pregen: %v", err)
		return nil
	}
	defer store.Close()

	if !forceRefresh {
		_, fresh, _, _, err := store.Get(proj.Hash)
		if err == nil && fresh {
			logFn("[debug] local cache is fresh, skipping pregen")
			return nil
		}
	}

	localCtx, localCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer localCancel()

	graph, err := local.BuildProjectGraph(localCtx, proj.RootDir, proj.Name)
	if err != nil {
		logFn("[warn] local graph build failed: %v", err)
		return nil
	}

	if err := store.Set(proj.Hash, proj.Name, graph); err != nil {
		logFn("[warn] cache write error: %v", err)
		return nil
	}

	logFn("[debug] local pregen complete: graph cached for %s", proj.Name)
	return nil
}
