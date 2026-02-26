package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var dryRunCmd = &cobra.Command{
	Use:   "dry-run",
	Short: "Show what would be injected without outputting it",
	Long:  `Runs the full Uncompact pipeline and displays the context bomb preview with metadata.`,
	RunE:  runDryRun,
}

func init() {
	rootCmd.AddCommand(dryRunCmd)
}

func runDryRun(cmd *cobra.Command, args []string) error {
	fmt.Println("=== Uncompact Dry Run ===")
	fmt.Printf("Max tokens: %d\n", maxTokens)
	fmt.Printf("Force refresh: %v\n\n", forceRefresh)
	fmt.Println("--- Context Bomb Preview ---")
	// Run the full pipeline but capture stdout
	return runRun(cmd, args)
}
