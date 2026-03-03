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
)

var (
	apiKey       string
	mode         string
	maxTokens    int
	forceRefresh bool
	fallback     bool
	debug        bool
)

var rootCmd = &cobra.Command{
	Use:   "uncompact",
	Short: "Re-inject project context into Claude Code after compaction",
	Long: `Uncompact prevents Claude Code compaction from degrading your AI workflow.
It hooks into Claude Code's lifecycle to reinject a high-density "context bomb"
at the right moment.

Modes:
  local  No API key required. Context bomb is generated from local repository
         analysis (file structure, git history, CLAUDE.md). This is the default
         when no API key is configured.

	api    Uses the Supermodel Public API for AI-powered summarization, smarter
         context prioritization, and session state analysis. Requires an API key.

Get started (local mode — no API key needed):
  uncompact install       # Add hooks to Claude Code settings.json

Get started (API mode — full AI-powered features):
  uncompact auth login    # Authenticate via dashboard.supermodeltools.com
  uncompact install       # Add hooks to Claude Code settings.json`,
	SilenceErrors:     true,
	SilenceUsage:      true,
	PersistentPreRunE: checkAuth,
}

func checkAuth(cmd *cobra.Command, args []string) error {
	// Skip auth check for auth commands, help, and completion
	for c := cmd; c != nil; c = c.Parent() {
		if c.Name() == "auth" || c.Name() == "help" || c.Name() == "completion" {
			return nil
		}
	}

	cfg, err := config.Load(apiKey)
	if err != nil {
		return err
	}

	if !cfg.IsAuthenticated() {
		return nil
	}

	keyHash := cfg.APIKeyHash()
	dbPath, _ := config.DBPath()
	var store *cache.Store
	if dbPath != "" {
		store, _ = cache.Open(dbPath)
	}

	if store != nil {
		defer store.Close()
		if auth, _ := store.GetAuthStatus(keyHash); auth != nil {
			// Cache is valid for 24h
			if time.Since(auth.LastValidatedAt) < 24*time.Hour {
				if auth.Identity != "" {
					fmt.Fprintf(os.Stderr, "[uncompact] Authenticated as %s\n", auth.Identity)
				}
				return nil
			}
		}
	}

	// Stale or missing cache, validate via API
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := api.New(cfg.BaseURL, cfg.APIKey, false, nil)
	identity, err := client.ValidateKey(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[uncompact] ⚠️ API key validation failed: %v\n", err)
		return nil // Don't block command execution on auth failure
	}

	if identity != "" {
		fmt.Fprintf(os.Stderr, "[uncompact] Authenticated as %s\n", identity)
		if store != nil {
			_ = store.SetAuthStatus(keyHash, identity)
		}
	}

	return nil
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&apiKey, "api-key", "", "Supermodel API key (overrides SUPERMODEL_API_KEY env var)")
	rootCmd.PersistentFlags().StringVar(&mode, "mode", "", `Operation mode: "local" (no API key required) or "api" (AI-powered). Default: auto-detect`)
	rootCmd.PersistentFlags().IntVar(&maxTokens, "max-tokens", config.DefaultMaxTokens, "Maximum tokens in context bomb output")
	rootCmd.PersistentFlags().BoolVar(&forceRefresh, "force-refresh", false, "Bypass cache and fetch fresh from API")
	rootCmd.PersistentFlags().BoolVar(&fallback, "fallback", false, "Emit minimal static context if full mode fails")
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "Enable debug output on stderr")
}
