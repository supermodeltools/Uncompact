package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/supermodeltools/uncompact/internal/project"
)

var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Manage the local Uncompact cache",
}

var cacheClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Clear the local cache for the current project",
	RunE:  runCacheClear,
}

var cacheAllFlag bool

func init() {
	cacheClearCmd.Flags().BoolVar(&cacheAllFlag, "all", false, "Clear cache for all projects")
	cacheCmd.AddCommand(cacheClearCmd)
	rootCmd.AddCommand(cacheCmd)
}

func runCacheClear(_ *cobra.Command, _ []string) error {
	db, err := openCache()
	if err != nil {
		return fmt.Errorf("opening cache: %w", err)
	}
	defer db.Close()

	if cacheAllFlag {
		if err := db.ClearAllProjects(); err != nil {
			return fmt.Errorf("clearing all cache: %w", err)
		}
		fmt.Println("✓ Cache cleared for all projects.")
		return nil
	}

	dir, err := project.RootDir()
	if err != nil {
		return err
	}
	projectHash, err := project.Hash(dir)
	if err != nil {
		return err
	}

	if err := db.ClearAll(projectHash); err != nil {
		return fmt.Errorf("clearing cache: %w", err)
	}
	fmt.Printf("✓ Cache cleared for project %s.\n", project.Name(dir))
	return nil
}
