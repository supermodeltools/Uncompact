package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "uncompact",
	Short: "Re-inject context into Claude Code after compaction",
	Long: `Uncompact prevents Claude Code compaction from degrading your workflow.
It generates a "context bomb" — a dense Markdown prompt re-injected into
your Claude Code session via hooks, powered by the Supermodel Public API.`,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().IntP("max-tokens", "t", 2000, "maximum tokens for context bomb output")
	rootCmd.PersistentFlags().Bool("force-refresh", false, "bypass cache and fetch fresh data from API")
	rootCmd.PersistentFlags().Bool("fallback", false, "emit minimal static context if full mode fails")
	rootCmd.PersistentFlags().Bool("debug", false, "enable debug logging")

	if err := rootCmd.PersistentFlags().MarkHidden("debug"); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not hide debug flag: %v\n", err)
	}
}
