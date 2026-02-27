package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/supermodeltools/uncompact/internal/hooks"
)

var installDryRun bool

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Add Uncompact hooks to Claude Code settings.json",
	Long: `Install detects your Claude Code settings.json and merges the Uncompact
Stop hook into it non-destructively. It shows a diff before writing.`,
	RunE: installHandler,
}

var verifyInstallCmd = &cobra.Command{
	Use:   "verify-install",
	Short: "Check if Uncompact hooks are properly installed",
	RunE:  verifyInstallHandler,
}

func init() {
	installCmd.Flags().BoolVar(&installDryRun, "dry-run", false, "Show what would be changed without writing")
	rootCmd.AddCommand(installCmd, verifyInstallCmd)
}

func installHandler(cmd *cobra.Command, args []string) error {
	settingsPath, err := hooks.FindSettingsFile()
	if err != nil {
		return fmt.Errorf("could not find Claude Code settings.json: %w\n\nPlease specify the path manually or ensure Claude Code is installed", err)
	}

	fmt.Printf("Settings file: %s\n\n", settingsPath)

	result, err := hooks.Install(settingsPath, true) // always dry-run first
	if err != nil {
		return fmt.Errorf("installing hooks: %w", err)
	}

	if result.AlreadySet {
		fmt.Println("✓ Uncompact hooks are already installed.")
		return nil
	}

	if installDryRun {
		fmt.Println("Changes that would be made (--dry-run mode):")
		fmt.Println()
		fmt.Println(result.Diff)
		fmt.Println()
		fmt.Println("Run 'uncompact install' without --dry-run to apply.")
		return nil
	}

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

	_, err = hooks.Install(settingsPath, false)
	if err != nil {
		return fmt.Errorf("writing settings.json: %w", err)
	}

	fmt.Println()
	fmt.Println("✓ Uncompact hooks installed successfully.")
	fmt.Println()
	fmt.Println("The Stop hook will now reinject context after Claude Code compaction.")
	fmt.Println("Test it: uncompact run --debug")
	return nil
}

func verifyInstallHandler(cmd *cobra.Command, args []string) error {
	settingsPath, err := hooks.FindSettingsFile()
	if err != nil {
		fmt.Println("✗ Could not find Claude Code settings.json")
		fmt.Println("  Ensure Claude Code is installed, or check your settings path.")
		return fmt.Errorf("settings file not found: %w", err)
	}

	installed, err := hooks.Verify(settingsPath)
	if err != nil {
		return fmt.Errorf("verifying hooks: %w", err)
	}

	if installed {
		fmt.Printf("✓ Uncompact hooks are installed in %s\n", settingsPath)
	} else {
		fmt.Printf("✗ Uncompact hooks are NOT installed.\n")
		fmt.Printf("  Run 'uncompact install' to add them to %s\n", settingsPath)
	}
	return nil
}
