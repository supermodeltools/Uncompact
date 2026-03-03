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
	uninstallTotal  bool
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove Uncompact hooks from Claude Code settings.json",
	Long: `Uninstall detects your Claude Code settings.json and removes the Uncompact
hooks from it, leaving any other hooks untouched. With --total, it also wipes
your local configuration and cached data.`,
	RunE: uninstallHandler,
}

func init() {
	uninstallCmd.Flags().BoolVar(&uninstallDryRun, "dry-run", false, "Show what would be changed without writing")
	uninstallCmd.Flags().BoolVarP(&uninstallYes, "yes", "y", false, "Skip confirmation prompt and apply changes")
	uninstallCmd.Flags().BoolVar(&uninstallTotal, "total", false, "Wipe all local configuration and cached data as well")
	rootCmd.AddCommand(uninstallCmd)
}

func uninstallHandler(cmd *cobra.Command, args []string) error {
	settingsPath, err := hooks.FindSettingsFile()
	if err != nil {
		return fmt.Errorf("could not find Claude Code settings.json: %w\n\nPlease specify the path manually or ensure Claude Code is installed", err)
	}

	if !uninstallTotal {
		fmt.Printf("Settings file: %s\n\n", settingsPath)
	}

	result, err := hooks.Uninstall(settingsPath, true) // always dry-run first
	if err != nil {
		return fmt.Errorf("uninstalling hooks: %w", err)
	}

	if uninstallDryRun {
		if !result.NothingToRemove {
			fmt.Println("Changes that would be made to settings.json:")
			fmt.Println()
			fmt.Println(result.Diff)
			fmt.Println()
		}
		if uninstallTotal {
			configDir, _ := config.ConfigDir()
			dataDir, _ := config.DataDir()
			fmt.Printf("Would also remove configuration directory: %s\n", configDir)
			fmt.Printf("Would also remove data/cache directory:    %s\n", dataDir)
			fmt.Println()
		}
		fmt.Println("Run 'uncompact uninstall' without --dry-run to apply.")
		return nil
	}

	if !uninstallYes {
		if !result.NothingToRemove {
			fmt.Println("The following changes will be made to settings.json:")
			fmt.Println()
			fmt.Println(result.Diff)
			fmt.Println()
		}
		prompt := "Apply these changes? [y/N]: "
		if uninstallTotal {
			prompt = "Wipe ALL Uncompact data and remove hooks? This cannot be undone. [y/N]: "
		}
		fmt.Print(prompt)

		scanner := bufio.NewScanner(os.Stdin)
		if !scanner.Scan() {
			return fmt.Errorf("no input")
		}
		answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
		if answer != "y" && answer != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	if !result.NothingToRemove {
		if uninstallYes {
			fmt.Println("Removing Uncompact hooks from settings.json...")
		}
		_, err = hooks.Uninstall(settingsPath, false)
		if err != nil {
			return fmt.Errorf("writing settings.json: %w", err)
		}
		fmt.Println("✓ Uncompact hooks removed successfully.")
	} else if !uninstallTotal {
		fmt.Println("✓ Uncompact hooks are not installed — nothing to remove.")
	}

	if uninstallTotal {
		configDir, err := config.ConfigDir()
		if err == nil {
			fmt.Printf("Removing configuration directory: %s\n", configDir)
			_ = os.RemoveAll(configDir)
		}
		dataDir, err := config.DataDir()
		if err == nil {
			fmt.Printf("Removing data/cache directory:    %s\n", dataDir)
			_ = os.RemoveAll(dataDir)
		}
		fmt.Println("✓ Local data wiped successfully.")
		fmt.Println()
		fmt.Println("To completely remove the CLI, run:")
		fmt.Println("  npm uninstall -g uncompact")
	}

	return nil
}
