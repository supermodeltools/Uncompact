package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/supermodeltools/uncompact/internal/template"
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
	logFn := makeLogger()
	cachePath := displayCachePath()

	// Atomically claim the cache file via rename so concurrent invocations
	// cannot both read it (TOCTOU race fix).
	tmpPath := cachePath + ".consuming"
	if err := os.Rename(cachePath, tmpPath); err != nil {
		if os.IsNotExist(err) {
			return nil // Another invocation already consumed it — silent exit.
		}
		return nil // Rename failed — silent exit to avoid blocking Claude Code.
	}

	data, err := os.ReadFile(tmpPath)
	if removeErr := os.Remove(tmpPath); removeErr != nil {
		logFn("[debug] failed to remove temp file %s: %v", tmpPath, removeErr)
	}
	if err != nil {
		return nil // Read error — silent exit to avoid blocking Claude Code.
	}

	if len(data) > 0 {
		approxTokens := template.CountTokens(string(data))
		fmt.Printf("%s\n\n[uncompact] Context restored (~%d tokens)\n", data, approxTokens)
	}
	return nil
}
