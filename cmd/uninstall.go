package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/supermodeltools/uncompact/internal/hooks"
)

var (
	uninstallDryRun bool
	uninstallYes    bool
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove Uncompact hooks from Claude Code settings.json",
	Long: `Uninstall detects your Claude Code settings.json and removes the Uncompact
hooks from it, leaving any other hooks untouched. It shows a diff before writing.`,
	RunE: uninstallHandler,
}

func init() {
	uninstallCmd.Flags().BoolVar(&uninstallDryRun, "dry-run", false, "Show what would be changed without writing")
	uninstallCmd.Flags().BoolVarP(&uninstallYes, "yes", "y", false, "Skip confirmation prompt and apply changes")
	rootCmd.AddCommand(uninstallCmd)
}

func uninstallHandler(cmd *cobra.Command, args []string) error {
	settingsPath, err := hooks.FindSettingsFile()
	if err != nil {
		return fmt.Errorf("could not find Claude Code settings.json: %w\n\nPlease specify the path manually or ensure Claude Code is installed", err)
	}

	fmt.Printf("Settings file: %s\n\n", settingsPath)

	result, err := hooks.Uninstall(settingsPath, true) // always dry-run first
	if err != nil {
		return fmt.Errorf("uninstalling hooks: %w", err)
	}

	if result.NothingToRemove {
		fmt.Println("✓ Uncompact hooks are not installed — nothing to remove.")
		return nil
	}

	if uninstallDryRun {
		fmt.Println("Changes that would be made (--dry-run mode):")
		fmt.Println()
		fmt.Println(result.Diff)
		fmt.Println()
		fmt.Println("Run 'uncompact uninstall' without --dry-run to apply.")
		return nil
	}

	if !uninstallYes {
		// Show diff and confirm
		fmt.Println("The following changes will be made to settings.json:")
		fmt.Println()
		fmt.Println(result.Diff)
		fmt.Println()
		fmt.Print("Apply these changes? [y/N]: ")

		scanner := bufio.NewScanner(os.Stdin)
		if !scanner.Scan() {
			return fmt.Errorf("no input")
		}
		answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
		if answer != "y" && answer != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
	} else {
		fmt.Println("Removing Uncompact hooks from settings.json...")
	}

	_, err = hooks.Uninstall(settingsPath, false)
	if err != nil {
		return fmt.Errorf("writing settings.json: %w", err)
	}

	fmt.Println()
	fmt.Println("✓ Uncompact hooks removed successfully.")
	return nil
}
