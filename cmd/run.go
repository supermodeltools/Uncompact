package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/supermodeltools/uncompact/internal/api"
	"github.com/supermodeltools/uncompact/internal/config"
	"github.com/supermodeltools/uncompact/internal/db"
	"github.com/supermodeltools/uncompact/internal/output"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Emit a context bomb to stdout (used by Claude Code hooks)",
	Long: `Generates and outputs a high-density Markdown context bomb to stdout.
This is the command invoked by Claude Code hooks to reinject context after compaction.

Exits 0 with no output if unable to generate context (never blocks a session).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil || cfg.APIKey == "" {
			debugLog("No API key configured; run 'uncompact auth login'")
			// Silent pass-through: never block the session
			return nil
		}

		store, err := db.Open(cfg.DBPath)
		if err != nil {
			debugLog("Failed to open cache DB: %v", err)
			return runFallback(cfg)
		}
		defer store.Close()

		// Rate limiting: skip injection if too recent
		if rateLimit > 0 {
			lastInjection, err := store.LastInjectionTime(workspaceHash())
			if err == nil && time.Since(lastInjection) < time.Duration(rateLimit)*time.Minute {
				debugLog("Rate limit: last injection was %v ago, skipping", time.Since(lastInjection))
				return nil
			}
		}

		client := api.NewClient(cfg)
		var graphData *api.GraphOutput
		var cacheHit bool

		if !forceRefresh {
			cached, err := store.GetCachedGraph(workspaceHash())
			if err == nil && cached != nil {
				graphData = cached
				cacheHit = true
				debugLog("Using cached graph (age: %v)", time.Since(cached.FetchedAt))
			}
		}

		if graphData == nil {
			graphData, err = client.GetContextGraph(cmd.Context(), workspaceContext())
			if err != nil {
				debugLog("API call failed: %v", err)
				// Try stale cache before fallback
				stale, cacheErr := store.GetStaleCachedGraph(workspaceHash())
				if cacheErr == nil && stale != nil {
					graphData = stale
					cacheHit = true
					fmt.Fprintln(os.Stderr, "[uncompact] WARNING: using stale cache (API unavailable)")
				} else if fallbackMode {
					return runFallback(cfg)
				} else {
					debugLog("No cache available and API failed; silent pass-through")
					return nil
				}
			}

			if !cacheHit {
				_ = store.CacheGraph(workspaceHash(), graphData)
			}
		}

		renderer := output.NewRenderer(maxTokens)
		bomb, tokenCount, err := renderer.Render(graphData)
		if err != nil {
			debugLog("Render failed: %v", err)
			return nil
		}

		fmt.Print(bomb)

		_ = store.LogInjection(db.InjectionEvent{
			WorkspaceHash:  workspaceHash(),
			TokensInjected: tokenCount,
			CacheHit:       cacheHit,
			InjectedAt:     time.Now(),
		})

		return nil
	},
}

func runFallback(cfg *config.Config) error {
	debugLog("Running in fallback mode")
	bomb := output.FallbackBomb(workspaceName(), maxTokens)
	if bomb != "" {
		fmt.Print(bomb)
	}
	return nil
}

func workspaceHash() string {
	wd, _ := os.Getwd()
	return db.HashWorkspace(wd)
}

func workspaceName() string {
	wd, _ := os.Getwd()
	if wd == "" {
		return "unknown"
	}
	for i := len(wd) - 1; i >= 0; i-- {
		if wd[i] == '/' || wd[i] == '\\' {
			return wd[i+1:]
		}
	}
	return wd
}

func workspaceContext() *api.WorkspaceContext {
	wd, _ := os.Getwd()
	return &api.WorkspaceContext{
		WorkspacePath: wd,
		WorkspaceName: workspaceName(),
	}
}

func debugLog(format string, args ...interface{}) {
	if debugMode {
		fmt.Fprintf(os.Stderr, "[uncompact debug] "+format+"\n", args...)
	}
}
