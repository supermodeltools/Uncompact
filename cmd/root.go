package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	apiKey      string
	maxTokens   int
	forceRefresh bool
)

var rootCmd = &cobra.Command{
	Use:   "uncompact",
	Short: "Re-inject rich code context into Claude Code after compaction",
	Long: `Uncompact prevents Claude Code compaction from degrading your workflow.
It analyzes your codebase via the Supermodel API and emits a high-density
"context bomb" in Markdown that Claude can use to restore its understanding.`,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&apiKey, "api-key", "", "Supermodel API key (overrides SUPERMODEL_API_KEY env var)")
	rootCmd.PersistentFlags().IntVar(&maxTokens, "max-tokens", 2000, "Maximum tokens for context bomb output")
	rootCmd.PersistentFlags().BoolVar(&forceRefresh, "force-refresh", false, "Bypass cache and fetch fresh data from API")
}
