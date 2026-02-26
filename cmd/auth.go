package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/supermodeltools/uncompact/internal/config"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage Supermodel API authentication",
}

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with the Supermodel API",
	Long: `Authenticates with the Supermodel API using a subscription key.

Get your API key from: ` + config.DashboardURL,
	RunE: runAuthLogin,
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current authentication status",
	RunE:  runAuthStatus,
}

func init() {
	authCmd.AddCommand(authLoginCmd)
	authCmd.AddCommand(authStatusCmd)
	rootCmd.AddCommand(authCmd)
}

func runAuthLogin(_ *cobra.Command, _ []string) error {
	fmt.Println("Supermodel API Authentication")
	fmt.Println("─────────────────────────────")
	fmt.Printf("Get your API key from: %s\n\n", config.DashboardURL)

	// Check if already configured
	existing, err := config.Load()
	if err == nil && existing.APIKey != "" {
		fmt.Println("An API key is already configured. Enter a new key to replace it, or press Enter to keep the existing one.")
	}

	fmt.Print("API Key: ")
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("reading input: %w", err)
	}
	key := strings.TrimSpace(input)

	if key == "" {
		if existing != nil && existing.APIKey != "" {
			fmt.Println("Keeping existing API key.")
			return nil
		}
		return fmt.Errorf("no API key provided")
	}

	cfg, _ := config.Load()
	if cfg == nil {
		cfg = &config.Config{}
	}
	cfg.APIKey = key

	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Println("✓ API key saved.")
	return nil
}

func runAuthStatus(_ *cobra.Command, _ []string) error {
	// Check env first
	if env := os.Getenv("SUPERMODEL_API_KEY"); env != "" {
		fmt.Printf("Authenticated via SUPERMODEL_API_KEY env var (key: %s...)\n", maskKey(env))
		return nil
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	if cfg.APIKey == "" {
		fmt.Println("Not authenticated.")
		fmt.Printf("Run `uncompact auth login` or set SUPERMODEL_API_KEY.\n")
		fmt.Printf("Get your API key from: %s\n", config.DashboardURL)
		return nil
	}

	fmt.Printf("Authenticated (config file)\n")
	fmt.Printf("Key: %s...\n", maskKey(cfg.APIKey))
	if cfg.APIBase != "" {
		fmt.Printf("API base: %s\n", cfg.APIBase)
	}
	return nil
}

// maskKey returns the first 8 characters of a key followed by ...
func maskKey(key string) string {
	if len(key) <= 8 {
		return "***"
	}
	return key[:8]
}
