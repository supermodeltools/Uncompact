package cmd

import (
	"context"
	"time"

	"github.com/spf13/cobra"
	"github.com/supermodeltools/uncompact/internal/api"
	"github.com/supermodeltools/uncompact/internal/cache"
	"github.com/supermodeltools/uncompact/internal/config"
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
		logFn("[warn] cannot open cache: %v", err)
		return pregenFetch(cfg, proj, logFn)
	}

	store, err := cache.Open(dbPath)
	if err != nil {
		logFn("[warn] cache open error: %v", err)
		return pregenFetch(cfg, proj, logFn)
	}
	defer store.Close()

	// Check if cache is already fresh — skip API call if so
	if !forceRefresh {
		_, fresh, _, err := store.Get(proj.Hash)
		if err == nil && fresh {
			logFn("[debug] cache is fresh, skipping pregen")
			return nil
		}
	}

	// Fetch from API with extended timeout (runs in background, so waiting is fine)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	logFn("[debug] fetching project graph from Supermodel API...")

	zipData, truncated, err := zip.RepoZip(proj.RootDir)
	if err != nil {
		logFn("[warn] zip error: %v", err)
		return nil
	}
	if truncated {
		logFn("[warn] repo zip truncated at 10 MB limit — large repos may produce incomplete graph analysis")
	}

	apiClient := api.New(cfg.BaseURL, cfg.APIKey, debug, logFn)
	graph, err := fetchGraphWithCircularDeps(ctx, apiClient, proj.Name, zipData, logFn)
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

// pregenFetch runs the API fetch without a cache (fallback when DB is unavailable).
func pregenFetch(cfg *config.Config, proj *project.Info, logFn func(string, ...interface{})) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	zipData, truncated, err := zip.RepoZip(proj.RootDir)
	if err != nil {
		logFn("[warn] zip error: %v", err)
		return nil
	}
	if truncated {
		logFn("[warn] repo zip truncated at 10 MB limit — large repos may produce incomplete graph analysis")
	}

	apiClient := api.New(cfg.BaseURL, cfg.APIKey, debug, logFn)
	_, err = fetchGraphWithCircularDeps(ctx, apiClient, proj.Name, zipData, logFn)
	if err != nil {
		logFn("[warn] API error: %v", err)
	}
	return nil
}
