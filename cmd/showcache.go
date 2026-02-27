package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var showCacheCmd = &cobra.Command{
	Use:           "show-cache",
	Short:         "Emit cached context bomb to stdout (used by the UserPromptSubmit hook)",
	SilenceErrors: true,
	SilenceUsage:  true,
	RunE:          showCacheHandler,
}

func init() {
	rootCmd.AddCommand(showCacheCmd)
}

func showCacheHandler(cmd *cobra.Command, args []string) error {
	cachePath := filepath.Join(os.TempDir(), fmt.Sprintf("uncompact-display-%d.txt", os.Getuid()))

	data, err := os.ReadFile(cachePath)
	if os.IsNotExist(err) {
		return nil // Nothing to show — silent exit.
	}
	if err != nil {
		return nil // Read error — silent exit to avoid blocking Claude Code.
	}

	// Consume the cache (one-shot display).
	os.Remove(cachePath)

	if len(data) > 0 {
		fmt.Print(string(data))
	}
	return nil
}
