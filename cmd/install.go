package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/supermodeltools/uncompact/internal/install"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install Uncompact hooks into Claude Code settings.json",
	Long: `Detect the Claude Code settings.json location, merge the Uncompact
hooks configuration without clobbering existing entries, and show a diff
for review before writing.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		return install.Run(dryRun)
	},
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Interactive setup wizard for first-time configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		return install.Init()
	},
}

var verifyCmd = &cobra.Command{
	Use:   "verify-install",
	Short: "Validate that hooks are correctly configured",
	RunE: func(cmd *cobra.Command, args []string) error {
		ok, issues := install.Verify()
		if len(issues) > 0 {
			for _, issue := range issues {
				fmt.Println("  ✗", issue)
			}
			return fmt.Errorf("installation verification failed")
		}
		if ok {
			fmt.Println("✓ Uncompact hooks are correctly configured")
		}
		return nil
	},
}

func init() {
	installCmd.Flags().Bool("dry-run", false, "show what would be written without making changes")
	rootCmd.AddCommand(installCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(verifyCmd)
}
