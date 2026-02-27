package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
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
	SilenceErrors: true,
	SilenceUsage:  true,
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
	rootCmd.PersistentFlags().IntVar(&maxTokens, "max-tokens", 2000, "Maximum tokens in context bomb output")
	rootCmd.PersistentFlags().BoolVar(&forceRefresh, "force-refresh", false, "Bypass cache and fetch fresh from API")
	rootCmd.PersistentFlags().BoolVar(&fallback, "fallback", false, "Emit minimal static context if full mode fails")
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "Enable debug output on stderr")
}
