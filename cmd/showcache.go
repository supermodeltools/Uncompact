package cmd

import (
	"fmt"
	"os"
	"os/user"
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
	uid := os.Getuid()
	var cacheID string
	if uid == -1 {
		// Windows: os.Getuid() always returns -1; fall back to username.
		if u, err := user.Current(); err == nil {
			cacheID = u.Username
		} else {
			cacheID = "windows"
		}
	} else {
		cacheID = fmt.Sprintf("%d", uid)
	}
	cachePath := filepath.Join(os.TempDir(), fmt.Sprintf("uncompact-display-%s.txt", cacheID))

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
		approxTokens := len(data) / 4
		fmt.Printf("%s\n\n[uncompact] Context restored (~%d tokens)\n", data, approxTokens)
	}
	return nil
}
