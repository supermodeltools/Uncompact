package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/supermodeltools/uncompact/internal/config"
	"github.com/supermodeltools/uncompact/internal/db"
)

var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Manage the local SQLite context cache",
}

var cacheClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Wipe all cached graph data",
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		cfg, err := config.Load()
		if err != nil {
			cfg = config.Default()
		}

		store, err := db.Open(cfg.DBPath)
		if err != nil {
			return fmt.Errorf("failed to open cache: %w", err)
		}
		defer store.Close()

		if all {
			if err := store.ClearAll(); err != nil {
				return fmt.Errorf("failed to clear cache: %w", err)
			}
			fmt.Println("✓ All cache entries cleared")
		} else {
			hash := workspaceHash()
			if err := store.ClearWorkspace(hash); err != nil {
				return fmt.Errorf("failed to clear workspace cache: %w", err)
			}
			fmt.Println("✓ Workspace cache cleared")
		}
		return nil
	},
}

var cachePruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Remove expired cache entries beyond the storage cap",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			cfg = config.Default()
		}

		store, err := db.Open(cfg.DBPath)
		if err != nil {
			return fmt.Errorf("failed to open cache: %w", err)
		}
		defer store.Close()

		removed, err := store.Prune()
		if err != nil {
			return fmt.Errorf("prune failed: %w", err)
		}
		fmt.Printf("✓ Pruned %d expired cache entries\n", removed)
		return nil
	},
}

func init() {
	cacheClearCmd.Flags().Bool("all", false, "Clear cache for all workspaces (not just current)")
	cacheCmd.AddCommand(cacheClearCmd)
	cacheCmd.AddCommand(cachePruneCmd)
}
