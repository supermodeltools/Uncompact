package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/supermodeltools/uncompact/internal/install"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Auto-detect Claude Code settings.json and merge Uncompact hooks",
	Long: `Detects the Claude Code settings.json location, merges Uncompact hooks
non-destructively, and prints a diff for review before writing.

This is the recommended way to install Uncompact hooks.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		yes, _ := cmd.Flags().GetBool("yes")
		return install.InstallHooks(yes)
	},
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Interactive setup wizard for first-time configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Uncompact Setup Wizard")
		fmt.Println("======================")
		fmt.Println()

		// Step 1: Authentication
		fmt.Println("Step 1: Authentication")
		fmt.Println("  Visit https://dashboard.supermodeltools.com to get an API key.")
		fmt.Println("  Then run: uncompact auth login")
		fmt.Println()

		// Step 2: Install hooks
		fmt.Println("Step 2: Install Claude Code hooks")
		fmt.Print("  Install hooks now? [Y/n]: ")

		var answer string
		fmt.Scanln(&answer)

		if answer == "" || answer == "y" || answer == "Y" {
			if err := install.InstallHooks(false); err != nil {
				fmt.Fprintf(os.Stderr, "Hook installation failed: %v\n", err)
				fmt.Println("  You can retry manually with: uncompact install")
			}
		} else {
			fmt.Println("  Skipped. Run 'uncompact install' when ready.")
		}

		fmt.Println()
		fmt.Println("Setup complete. Run 'uncompact status' to verify.")
		return nil
	},
}

var verifyInstallCmd = &cobra.Command{
	Use:   "verify-install",
	Short: "Validate that hooks are correctly configured in settings.json",
	RunE: func(cmd *cobra.Command, args []string) error {
		issues, err := install.VerifyHooks()
		if err != nil {
			return fmt.Errorf("verification failed: %w", err)
		}

		if len(issues) == 0 {
			fmt.Println("✓ Uncompact hooks are correctly configured")
			settingsPath, _ := install.FindSettingsJSON()
			if settingsPath != "" {
				fmt.Println("  Settings file:", settingsPath)
			}
			return nil
		}

		fmt.Fprintln(os.Stderr, "✗ Hook configuration issues found:")
		for _, issue := range issues {
			fmt.Fprintf(os.Stderr, "  - %s\n", issue)
		}
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Run 'uncompact install' to fix.")
		os.Exit(1)
		return nil
	},
}

func init() {
	installCmd.Flags().BoolP("yes", "y", false, "Skip diff confirmation and write immediately")
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(verifyInstallCmd)
}
