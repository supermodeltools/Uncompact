package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/supermodeltools/uncompact/internal/api"
	"github.com/supermodeltools/uncompact/internal/cache"
	"github.com/supermodeltools/uncompact/internal/config"
	"github.com/supermodeltools/uncompact/internal/project"
	"github.com/supermodeltools/uncompact/internal/template"
	"github.com/supermodeltools/uncompact/internal/zip"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Emit a context bomb to stdout (used by Claude Code hooks)",
	Long: `Analyzes the current repository via the Supermodel API and outputs
a high-density Markdown context bomb to stdout. This is the command
invoked by the Claude Code hooks to re-inject context after compaction.`,
	RunE: runRun,
	// Silence usage on error — hook output should be clean
	SilenceUsage: true,
}

func init() {
	rootCmd.AddCommand(runCmd)
}

func runRun(cmd *cobra.Command, args []string) error {
	// Resolve working directory
	dir, err := project.RootDir()
	if err != nil {
		return silentExit(err)
	}

	// Resolve project identity
	projectHash, err := project.Hash(dir)
	if err != nil {
		return silentExit(err)
	}
	projectName := project.Name(dir)

	// Open cache
	db, err := openCache()
	if err != nil {
		// Non-fatal: proceed without cache
		fmt.Fprintln(os.Stderr, "[uncompact] cache unavailable:", err)
	}
	if db != nil {
		defer db.Close()
		// Prune on run (background cleanup)
		_ = db.Prune()
	}

	var ir *api.SupermodelIR
	var stale bool
	var cacheHit bool

	// Check cache first (unless force-refresh)
	if db != nil && !forceRefresh {
		entry, err := db.Get(projectHash, "supermodel", config.DefaultTTLSeconds)
		if err == nil && entry != nil {
			if err := json.Unmarshal([]byte(entry.ResponseJSON), &ir); err == nil {
				cacheHit = true
			}
		}
	}

	// Fetch from API if cache miss
	if ir == nil {
		key, err := config.ResolveAPIKey(apiKey)
		if err != nil {
			// No API key — try stale cache before failing silently
			if db != nil {
				entry, _ := db.GetStale(projectHash, "supermodel")
				if entry != nil {
					_ = json.Unmarshal([]byte(entry.ResponseJSON), &ir)
					stale = true
				}
			}
			if ir == nil {
				return silentExit(fmt.Errorf("no API key: %w", err))
			}
		}

		if ir == nil {
			cfg, _ := config.Load()
			baseURL := config.ResolveAPIBase(cfg)
			client := api.New(key, baseURL)

			// Zip the repository
			repoZip, err := zip.ZipDir(dir)
			if err != nil {
				return silentExit(err)
			}

			// Generate idempotency key from project hash + zip size
			idempotencyKey := fmt.Sprintf("uncompact-%s-%d", projectHash, len(repoZip))

			ir, err = client.GenerateSupermodelIR(repoZip, idempotencyKey)
			if err != nil {
				// API failed — try stale cache
				if db != nil {
					entry, _ := db.GetStale(projectHash, "supermodel")
					if entry != nil {
						_ = json.Unmarshal([]byte(entry.ResponseJSON), &ir)
						stale = true
						fmt.Fprintf(os.Stderr, "[uncompact] API unavailable (%v), using stale cache\n", err)
					}
				}
				if ir == nil {
					return silentExit(err)
				}
			}

			// Cache the fresh result
			if db != nil && !stale {
				_ = db.Set(projectHash, "supermodel", ir, config.DefaultTTLSeconds)
			}
		}
	}

	// Render context bomb
	output, err := template.RenderContextBomb(ir, projectName, stale, maxTokens)
	if err != nil {
		return silentExit(err)
	}

	// Log the injection
	if db != nil {
		_ = db.LogInjection(projectHash, template.EstimateTokens(output), cacheHit)
	}

	fmt.Print(output)
	return nil
}

// silentExit logs to stderr and returns nil so the hook doesn't fail the session.
func silentExit(err error) error {
	if err != nil {
		fmt.Fprintln(os.Stderr, "[uncompact] warning:", err)
	}
	return nil
}

// openCache opens the SQLite cache database.
func openCache() (*cache.DB, error) {
	cacheDir, err := config.CacheDir()
	if err != nil {
		return nil, err
	}
	dbPath := filepath.Join(cacheDir, "uncompact.db")
	return cache.Open(dbPath)
}
