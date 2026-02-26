package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/supermodeltools/uncompact/internal/api"
	"github.com/supermodeltools/uncompact/internal/db"
	"github.com/supermodeltools/uncompact/internal/output"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Generate and output a context bomb",
	Long:  `Fetch context from the Supermodel API (or cache), render it as Markdown, and print to stdout for hook injection.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		maxTokens, _ := cmd.Flags().GetInt("max-tokens")
		forceRefresh, _ := cmd.Flags().GetBool("force-refresh")
		fallback, _ := cmd.Flags().GetBool("fallback")

		store, err := db.Open("")
		if err != nil {
			if fallback {
				fmt.Print(output.FallbackContext())
				return nil
			}
			return nil // silent pass-through — don't block Claude session
		}
		defer store.Close()

		client := api.NewClient()

		graph, err := client.FetchGraph(cmd.Context(), forceRefresh, store)
		if err != nil {
			if fallback {
				fmt.Print(output.FallbackContext())
				return nil
			}
			// Serve stale cache if available
			cached, cacheErr := store.GetLatest()
			if cacheErr != nil || cached == nil {
				return nil // silent pass-through
			}
			graph, err = api.GraphFromRecord(cached)
			if err != nil {
				return nil
			}
		}

		bomb, err := output.RenderContextBomb(graph, maxTokens)
		if err != nil {
			return nil // silent pass-through
		}

		fmt.Print(bomb)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(runCmd)
}
