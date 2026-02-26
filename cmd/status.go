package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/supermodeltools/uncompact/internal/config"
	"github.com/supermodeltools/uncompact/internal/project"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show last injection time, cache state, and token size",
	RunE:  runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(_ *cobra.Command, _ []string) error {
	dir, err := project.RootDir()
	if err != nil {
		return err
	}
	projectHash, err := project.Hash(dir)
	if err != nil {
		return err
	}
	projectName := project.Name(dir)

	fmt.Printf("Project: %s (%s)\n", projectName, projectHash)
	fmt.Printf("Dir:     %s\n\n", dir)

	// Auth status
	key, err := config.ResolveAPIKey(apiKey)
	if err != nil {
		fmt.Println("Auth:    ✗ not configured — run `uncompact auth login`")
	} else {
		fmt.Printf("Auth:    ✓ API key configured (%s...)\n", maskKeyStatus(key))
	}

	// Cache status
	db, err := openCache()
	if err != nil {
		fmt.Println("Cache:   unavailable")
		return nil
	}
	defer db.Close()

	entry, err := db.Get(projectHash, "supermodel", config.DefaultTTLSeconds)
	if err != nil || entry == nil {
		staleEntry, _ := db.GetStale(projectHash, "supermodel")
		if staleEntry != nil {
			age := time.Since(staleEntry.CreatedAt)
			fmt.Printf("Cache:   stale (last updated %s ago)\n", formatDuration(age))
		} else {
			fmt.Println("Cache:   empty — run `uncompact run` to populate")
		}
	} else {
		age := time.Since(entry.CreatedAt)
		fmt.Printf("Cache:   ✓ fresh (cached %s ago, TTL %ds)\n", formatDuration(age), config.DefaultTTLSeconds)
	}

	// Last injection
	lastTime, tokens, cacheHit, err := db.LastInjection(projectHash)
	if err == nil && !lastTime.IsZero() {
		hitStr := "API"
		if cacheHit {
			hitStr = "cache"
		}
		fmt.Printf("Last:    %s ago (~%d tokens, from %s)\n",
			formatDuration(time.Since(lastTime)), tokens, hitStr)
	} else {
		fmt.Println("Last:    never injected")
	}

	return nil
}

func maskKeyStatus(key string) string {
	if len(key) <= 8 {
		return "***"
	}
	return key[:8]
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
