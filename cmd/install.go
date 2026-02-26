package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/supermodeltools/uncompact/internal/hooks"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install Uncompact hooks into Claude Code settings.json",
	Long: `Detects the Claude Code settings.json location and merges
Uncompact hooks non-destructively. Shows a diff before writing.

Add --dry-run to preview changes without writing.`,
	RunE: runInstall,
}

var installDryRun bool

func init() {
	installCmd.Flags().BoolVar(&installDryRun, "dry-run", false, "Preview changes without writing")
	rootCmd.AddCommand(installCmd)
}

func runInstall(_ *cobra.Command, _ []string) error {
	result, err := hooks.Install(maxTokens, installDryRun)
	if err != nil {
		return err
	}

	fmt.Printf("Settings file: %s\n\n", result.SettingsPath)
	fmt.Println("Changes:")
	fmt.Println(result.Diff)

	if installDryRun {
		fmt.Println("Dry run — no changes written. Remove --dry-run to apply.")
	} else if result.WroteFile {
		fmt.Println("✓ settings.json updated.")
		fmt.Println()
		fmt.Println("Uncompact will now reinject context after compaction.")
		fmt.Println("Run `uncompact status` to verify.")
	}
	return nil
}
