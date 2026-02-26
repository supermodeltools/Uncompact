package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/supermodeltools/uncompact/internal/api"
	"github.com/supermodeltools/uncompact/internal/config"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage authentication with the Supermodel API",
}

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate via dashboard.supermodeltools.com",
	Long: `Opens your browser to dashboard.supermodeltools.com where you can subscribe
and generate an API key. Paste the key when prompted.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			cfg = config.Default()
		}

		fmt.Println("To authenticate with Uncompact:")
		fmt.Println()
		fmt.Println("  1. Visit: https://dashboard.supermodeltools.com")
		fmt.Println("  2. Create an account or log in")
		fmt.Println("  3. Subscribe and generate an API key")
		fmt.Println("  4. Paste your API key below")
		fmt.Println()
		fmt.Print("API key: ")

		var key string
		if _, err := fmt.Scanln(&key); err != nil {
			return fmt.Errorf("failed to read API key: %w", err)
		}

		if len(key) < 10 {
			return fmt.Errorf("invalid API key: too short")
		}

		cfg.APIKey = key
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}

		// Validate the key against the API
		client := api.NewClient(cfg)
		if err := client.ValidateAuth(cmd.Context()); err != nil {
			// Remove the saved key on validation failure
			cfg.APIKey = ""
			_ = config.Save(cfg)
			return fmt.Errorf("authentication failed: %w", err)
		}

		fmt.Println()
		fmt.Println("✓ Authentication successful. Key saved to", config.ConfigPath())
		return nil
	},
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current auth status and subscription tier",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintln(os.Stderr, "Not authenticated. Run: uncompact auth login")
			os.Exit(1)
		}

		if cfg.APIKey == "" {
			fmt.Println("Status: not authenticated")
			fmt.Println("Run: uncompact auth login")
			return nil
		}

		client := api.NewClient(cfg)
		info, err := client.GetAccountInfo(cmd.Context())
		if err != nil {
			fmt.Printf("Status: authenticated (key present, API unreachable: %v)\n", err)
			return nil
		}

		fmt.Printf("Status: authenticated\n")
		fmt.Printf("Account: %s\n", info.Email)
		fmt.Printf("Tier:    %s\n", info.SubscriptionTier)
		if info.APICallsRemaining >= 0 {
			fmt.Printf("API calls remaining: %d\n", info.APICallsRemaining)
		}
		return nil
	},
}

func init() {
	authCmd.AddCommand(authLoginCmd)
	authCmd.AddCommand(authStatusCmd)
}
