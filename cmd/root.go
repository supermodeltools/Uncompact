package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	maxTokens    int
	forceRefresh bool
	fallbackMode bool
	debugMode    bool
	rateLimit    int
)

var rootCmd = &cobra.Command{
	Use:   "uncompact",
	Short: "Reinject high-density context into Claude Code after compaction",
	Long: `Uncompact hooks into Claude Code's lifecycle to reinject a "context bomb"
after compaction, preventing context loss from degrading agent quality.

It is powered by the Supermodel Public API. Authentication is managed via
a subscription at https://dashboard.supermodeltools.com.`,
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().IntVar(&maxTokens, "max-tokens", 2000,
		"Maximum tokens in context bomb output")
	rootCmd.PersistentFlags().BoolVar(&forceRefresh, "force-refresh", false,
		"Bypass cache and fetch fresh data from API")
	rootCmd.PersistentFlags().BoolVar(&fallbackMode, "fallback", false,
		"Emit minimal static context if full API mode fails")
	rootCmd.PersistentFlags().BoolVar(&debugMode, "debug", false,
		"Enable debug logging to stderr")
	rootCmd.PersistentFlags().IntVar(&rateLimit, "rate-limit", 5,
		"Minimum minutes between injections per workspace (0 to disable)")

	rootCmd.AddCommand(authCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(installCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(logsCmd)
	rootCmd.AddCommand(dryRunCmd)
	rootCmd.AddCommand(cacheCmd)
	rootCmd.AddCommand(statsCmd)
}
