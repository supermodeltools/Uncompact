package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/supermodeltools/uncompact/internal/api"
	"github.com/supermodeltools/uncompact/internal/config"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage Supermodel API authentication",
}

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate via dashboard.supermodeltools.com",
	RunE:  authLoginHandler,
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current authentication status",
	RunE:  authStatusHandler,
}

var authLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Remove stored API key",
	RunE:  authLogoutHandler,
}

func init() {
	authCmd.AddCommand(authLoginCmd, authStatusCmd, authLogoutCmd)
	rootCmd.AddCommand(authCmd)
}

func authLoginHandler(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(apiKey)
	if err != nil {
		return err
	}

	fmt.Println("Uncompact uses the Supermodel Public API.")
	fmt.Println()
	fmt.Println("1. Visit the dashboard to get your API key:")
	fmt.Println("   " + config.DashboardURL)
	fmt.Println()
	fmt.Print("2. Paste your API key here: ")

	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("reading API key: %w", err)
		}
		return fmt.Errorf("no input provided")
	}
	key := strings.TrimSpace(scanner.Text())
	if key == "" {
		return fmt.Errorf("API key cannot be empty")
	}

	// Validate the key
	fmt.Print("Validating key... ")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	testClient := api.New(cfg.BaseURL, key, false, nil)
	identity, err := testClient.ValidateKey(ctx)
	if err != nil {
		fmt.Println("✗")
		return fmt.Errorf("key validation failed: %w", err)
	}
	fmt.Println("✓")

	if identity != "" {
		fmt.Printf("Authenticated as: %s\n", identity)
	}

	// Save
	cfg.APIKey = key
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	cfgFile, _ := config.ConfigFile()
	fmt.Printf("\nAPI key saved to: %s\n", cfgFile)
	fmt.Println()
	fmt.Println("Next: run 'uncompact install' to add hooks to Claude Code.")
	return nil
}

func authStatusHandler(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(apiKey)
	if err != nil {
		return err
	}

	if !cfg.IsAuthenticated() {
		fmt.Println("Status: not authenticated")
		fmt.Println()
		fmt.Println("Run 'uncompact auth login' to authenticate.")
		return nil
	}

	fmt.Printf("Status: authenticated\n")
	keyLen := len(cfg.APIKey)
	if keyLen <= 8 {
		fmt.Printf("API key: [%d chars]\n", keyLen)
	} else {
		fmt.Printf("API key: %s...%s\n",
			cfg.APIKey[:4],
			cfg.APIKey[keyLen-4:],
		)
	}

	// Try to validate
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := api.New(cfg.BaseURL, cfg.APIKey, false, nil)
	identity, err := client.ValidateKey(ctx)
	if err != nil {
		fmt.Printf("API check: ✗ (%v)\n", err)
	} else {
		fmt.Printf("API check: ✓\n")
		if identity != "" {
			fmt.Printf("Identity:  %s\n", identity)
		}
	}
	return nil
}

func authLogoutHandler(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(apiKey)
	if err != nil {
		return err
	}
	cfg.APIKey = ""
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}
	fmt.Println("API key removed.")
	return nil
}

