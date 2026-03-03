package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/pkg/browser"
	"github.com/spf13/cobra"
	"github.com/supermodeltools/uncompact/internal/api"
	"github.com/supermodeltools/uncompact/internal/cache"
	"github.com/supermodeltools/uncompact/internal/config"
	"golang.org/x/term"
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

var authOpenCmd = &cobra.Command{
	Use:   "open",
	Short: "Open the Supermodel dashboard in your browser",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Opening " + config.DashboardKeyURL + "...")
		_ = browser.OpenURL(config.DashboardKeyURL)
	},
}

func init() {
	authCmd.AddCommand(authLoginCmd, authStatusCmd, authLogoutCmd, authOpenCmd)
	rootCmd.AddCommand(authCmd)
}

func authLoginHandler(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(apiKey)
	if err != nil {
		return err
	}

	fmt.Println("Uncompact uses the Supermodel Public API.")
	fmt.Println()
	fmt.Println("1. Opening your browser to the Supermodel dashboard...")
	fmt.Println("   " + config.DashboardKeyURL)
	fmt.Println()

	_ = browser.OpenURL(config.DashboardKeyURL)

	fmt.Println("2. Sign in, create an API key, and paste it below.")
	fmt.Println("   (The input will be hidden for security)")
	fmt.Print("   API Key: ")

	var key string
	if term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return fmt.Errorf("reading API key: %w", err)
		}
		key = strings.TrimSpace(string(b))
	} else {
		// Non-interactive fallback (e.g. piped input in CI)
		var raw string
		if _, err := fmt.Fscanln(os.Stdin, &raw); err != nil {
			return fmt.Errorf("reading API key: %w", err)
		}
		key = strings.TrimSpace(raw)
	}
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
		if strings.Contains(err.Error(), "402") {
			fmt.Println()
			fmt.Println("⚠️  SUBSCRIPTION REQUIRED")
			fmt.Println("   The API key is valid, but your account requires an active subscription.")
			fmt.Printf("   Please visit: %s\n", config.DashboardURL)
			fmt.Println()
			return fmt.Errorf("subscription required")
		}
		return fmt.Errorf("key validation failed: %w", err)
	}
	fmt.Println("✓")

	if identity != "" {
		fmt.Printf("Authenticated as: %s\n", identity)
	}

	// Warn if environment variable is masking
	if os.Getenv(config.EnvAPIKey) != "" {
		fmt.Println()
		fmt.Printf("⚠️  WARNING: The environment variable %s is currently set.\n", config.EnvAPIKey)
		fmt.Println("   It will continue to override the API key you just saved to the config file.")
		fmt.Println("   To use the new key, you must unset the environment variable or update it.")
	}

	// Save
	cfg.APIKey = key
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	// Cache the auth status
	dbPath, err := config.DBPath()
	if err == nil {
		if store, err := cache.Open(dbPath); err == nil {
			defer store.Close()
			_ = store.SetAuthStatus(cfg.APIKeyHash(), identity)
		}
	}

	if cfgFile, err := config.ConfigFile(); err == nil {
		fmt.Printf("\nAPI key saved to: %s\n", cfgFile)
	} else {
		fmt.Println("\nAPI key saved.")
	}
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
	fmt.Printf("Source: %s\n", cfg.Source)
	keyLen := len(cfg.APIKey)
	if keyLen <= 8 {
		fmt.Printf("API key: [%d chars]\n", keyLen)
	} else {
		fmt.Printf("API key: %s...%s\n",
			cfg.APIKey[:4],
			cfg.APIKey[keyLen-4:],
		)
	}

	// Try to get from cache first
	dbPath, _ := config.DBPath()
	var store *cache.Store
	if dbPath != "" {
		store, _ = cache.Open(dbPath)
	}
	if store != nil {
		defer store.Close()
		if auth, _ := store.GetAuthStatus(cfg.APIKeyHash()); auth != nil {
			// Only use cache if it's less than 24h old
			if time.Since(auth.LastValidatedAt) < 24*time.Hour {
				fmt.Printf("API check: ✓ (cached %s ago)\n", humanDuration(time.Since(auth.LastValidatedAt)))
				if auth.Identity != "" {
					fmt.Printf("Identity:  %s\n", auth.Identity)
				}
				return nil
			}
		}
	}

	// Not in cache or stale, validate via API
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
		// Update cache
		if store != nil {
			_ = store.SetAuthStatus(cfg.APIKeyHash(), identity)
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

