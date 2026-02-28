package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/supermodeltools/uncompact/internal/detect"
	"github.com/supermodeltools/uncompact/internal/fsutil"
	"github.com/supermodeltools/uncompact/internal/hooks"
)

var (
	initYes     bool
	initNoHooks bool
	initForce   bool
	initDryRun  bool
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Generate a CLAUDE.md and configure Uncompact hooks for this repository",
	Long: `Init analyzes the current repository and generates a tailored CLAUDE.md
with sensible defaults for your language, build system, and toolchain.
It also configures the Uncompact hooks in .claude/settings.json.`,
	RunE: initHandler,
}

func init() {
	initCmd.Flags().BoolVarP(&initYes, "yes", "y", false, "Non-interactive: accept all defaults")
	initCmd.Flags().BoolVar(&initNoHooks, "no-hooks", false, "Generate CLAUDE.md only, skip hook configuration")
	initCmd.Flags().BoolVar(&initForce, "force", false, "Overwrite existing CLAUDE.md")
	initCmd.Flags().BoolVar(&initDryRun, "dry-run", false, "Print what would be generated without writing files")
	rootCmd.AddCommand(initCmd)
}

func initHandler(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	fmt.Println("Analyzing repository...")
	info := detect.Analyze(cwd)

	fmt.Printf("  ✓ Detected: %s\n", info.LanguageSummary())
	if info.BuildCmd != "" {
		fmt.Printf("  ✓ Build:    %s\n", info.BuildCmd)
	}
	if info.LintCmd != "" {
		fmt.Printf("  ✓ Lint:     %s\n", info.LintCmd)
	}
	if info.TestCmd != "" {
		fmt.Printf("  ✓ Test:     %s\n", info.TestCmd)
	} else {
		fmt.Println("  ✓ Test:     no test suite detected")
	}
	if _, statErr := os.Stat(filepath.Join(cwd, ".claude")); statErr == nil {
		fmt.Println("  ✓ Found: existing .claude/ directory")
	}
	fmt.Println()

	claudeMDPath := filepath.Join(cwd, "CLAUDE.md")
	claudeMDExists := fsutil.FileExists(claudeMDPath)

	content := info.GenerateCLAUDEMD()

	// Handle CLAUDE.md generation.
	if claudeMDExists && !initForce && !initDryRun {
		fmt.Println("⚠  CLAUDE.md already exists.")
		fmt.Println("   Use --force to overwrite.")
		fmt.Println()
		if !initYes {
			fmt.Print("Show generated content anyway? [y/N]: ")
			scanner := bufio.NewScanner(os.Stdin)
			if scanner.Scan() {
				answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
				if answer == "y" || answer == "yes" {
					fmt.Println()
					fmt.Println(content)
					fmt.Println()
				}
			}
		}
	} else {
		fmt.Println("Generating CLAUDE.md...")
		if initDryRun {
			fmt.Println("  (dry-run) Would write: CLAUDE.md")
			fmt.Println()
			fmt.Println(content)
		} else {
			// Confirm unless --yes or overwriting (force implies consent).
			if !initYes && !initForce {
				fmt.Print("Write CLAUDE.md? [Y/n]: ")
				scanner := bufio.NewScanner(os.Stdin)
				if scanner.Scan() {
					answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
					if answer == "n" || answer == "no" {
						fmt.Println("Aborted.")
						return nil
					}
				}
			}
			if err := os.WriteFile(claudeMDPath, []byte(content), 0644); err != nil {
				return fmt.Errorf("writing CLAUDE.md: %w", err)
			}
			fmt.Println("  ✓ Written: CLAUDE.md")
		}
		fmt.Println()
	}

	// Configure Uncompact hooks.
	if !initNoHooks {
		fmt.Println("Configuring Uncompact hooks...")
		settingsPath, hookErr := hooks.FindSettingsFile()
		if hookErr != nil {
			fmt.Println("  ✗ Could not find Claude Code settings.json — skipping hook configuration")
			fmt.Println("    Run 'uncompact install' later to add hooks manually.")
		} else if initDryRun {
			fmt.Printf("  (dry-run) Would update: %s\n", settingsPath)
		} else {
			result, installErr := hooks.Install(settingsPath, false)
			if installErr != nil {
				fmt.Printf("  ✗ Hook configuration failed: %v\n", installErr)
				fmt.Println("    Run 'uncompact install' later to add hooks manually.")
			} else if result.AlreadySet {
				fmt.Printf("  ✓ Already configured: %s\n", settingsPath)
			} else {
				fmt.Printf("  ✓ Updated: %s\n", settingsPath)
			}
		}
		fmt.Println()
	}

	// Print next steps.
	fmt.Println("Next steps:")
	fmt.Println("  • Review and customize CLAUDE.md for your project")
	fmt.Println("  • Run `git add CLAUDE.md .claude/settings.json && git commit`")
	fmt.Println("  • Add the Uncompact badge to your README:")
	fmt.Println(`    [![](https://img.shields.io/badge/context--bomb-Uncompact-blue)](https://github.com/supermodeltools/Uncompact)`)
	return nil
}

