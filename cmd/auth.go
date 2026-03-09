package cmd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
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

var noBrowser bool

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
	authLoginCmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Skip browser-based login and paste API key manually")
	authCmd.AddCommand(authLoginCmd, authStatusCmd, authLogoutCmd, authOpenCmd)
	rootCmd.AddCommand(authCmd)
}

const callbackTimeout = 2 * time.Minute

const successHTML = `<!DOCTYPE html>
<html>
<head><title>Uncompact</title>
<style>
  body { font-family: system-ui, sans-serif; display: flex; justify-content: center;
         align-items: center; min-height: 100vh; margin: 0; background: #0f172a; color: #e2e8f0; }
  .card { text-align: center; padding: 2rem; }
  h1 { color: #5eead4; margin-bottom: 0.5rem; }
  p { color: #94a3b8; }
</style>
</head>
<body>
<div class="card">
  <h1>Authenticated</h1>
  <p>You can close this tab and return to your terminal.</p>
</div>
</body>
</html>`

func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func authLoginHandler(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(apiKey)
	if err != nil {
		return err
	}

	isTTY := term.IsTerminal(int(os.Stdin.Fd()))

	if noBrowser || !isTTY {
		return authLoginManual(cfg)
	}

	key, err := authLoginBrowser(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[uncompact] Browser login failed: %v\n", err)
		fmt.Println()
		fmt.Println("Falling back to manual login...")
		fmt.Println()
		return authLoginManual(cfg)
	}

	return saveAndCacheKey(cfg, key)
}

// authLoginBrowser starts a localhost callback server, opens the dashboard,
// and waits for the key to arrive via redirect.
func authLoginBrowser(cfg *config.Config) (string, error) {
	logFn := makeLogger()

	state, err := generateState()
	if err != nil {
		return "", fmt.Errorf("generating state: %w", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("starting callback server: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	logFn("[debug] auth: callback server listening on 127.0.0.1:%d", port)

	type callbackResult struct {
		key string
		err error
	}
	resultCh := make(chan callbackResult, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		logFn("[debug] auth: callback received: %s", r.URL.String())

		gotState := r.URL.Query().Get("state")
		if gotState != state {
			logFn("[debug] auth: state mismatch — got %q, expected %q", gotState, state)
			http.Error(w, "Invalid state parameter", http.StatusForbidden)
			resultCh <- callbackResult{err: fmt.Errorf("state mismatch (possible CSRF)")}
			return
		}

		key := r.URL.Query().Get("key")
		if key == "" {
			errMsg := r.URL.Query().Get("error")
			if errMsg == "" {
				errMsg = "no key received"
			}
			logFn("[debug] auth: callback error — %s", errMsg)
			http.Error(w, errMsg, http.StatusBadRequest)
			resultCh <- callbackResult{err: fmt.Errorf("dashboard returned error: %s", errMsg)}
			return
		}

		logFn("[debug] auth: key received (%d chars)", len(key))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, successHTML)

		resultCh <- callbackResult{key: key}
	})

	server := &http.Server{Handler: mux}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			resultCh <- callbackResult{err: fmt.Errorf("callback server error: %w", err)}
		}
	}()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()

	dashURL := fmt.Sprintf("%s?port=%d&state=%s", config.EffectiveCLIAuthURL(), port, state)
	logFn("[debug] auth: dashboard URL: %s", dashURL)
	fmt.Println("Opening your browser to sign in...")
	fmt.Printf("  %s\n\n", dashURL)
	fmt.Println("Waiting for authentication (this will timeout in 2 minutes)...")

	if err := browser.OpenURL(dashURL); err != nil {
		return "", fmt.Errorf("opening browser: %w", err)
	}

	select {
	case result := <-resultCh:
		if result.err != nil {
			return "", result.err
		}
		fmt.Println()
		fmt.Print("Validating key... ")

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		testClient := api.New(cfg.BaseURL, result.key, false, nil)
		identity, err := testClient.ValidateKey(ctx)
		if err != nil {
			fmt.Println("✗")
			return "", fmt.Errorf("key validation failed: %w", err)
		}
		fmt.Println("✓")
		if identity != "" {
			fmt.Printf("Authenticated as: %s\n", identity)
		}
		return result.key, nil

	case <-time.After(callbackTimeout):
		return "", fmt.Errorf("timed out waiting for browser callback")
	}
}

// authLoginManual is the original paste-based login flow.
func authLoginManual(cfg *config.Config) error {
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
		var raw string
		if _, err := fmt.Fscanln(os.Stdin, &raw); err != nil {
			return fmt.Errorf("reading API key: %w", err)
		}
		key = strings.TrimSpace(raw)
	}
	if key == "" {
		return fmt.Errorf("API key cannot be empty")
	}

	fmt.Print("Validating key... ")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	testClient := api.New(cfg.BaseURL, key, false, nil)
	identity, err := testClient.ValidateKey(ctx)
	if err != nil {
		fmt.Println("✗")
		if strings.Contains(err.Error(), "402") {
			fmt.Println()
			fmt.Println("SUBSCRIPTION REQUIRED")
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

	return saveAndCacheKey(cfg, key)
}

// saveAndCacheKey encrypts and saves the key, then updates the auth cache.
func saveAndCacheKey(cfg *config.Config, key string) error {
	if os.Getenv(config.EnvAPIKey) != "" {
		fmt.Println()
		fmt.Printf("NOTE: The environment variable %s is currently set.\n", config.EnvAPIKey)
		fmt.Println("   It will continue to override the API key saved to the config file.")
		fmt.Println("   To use the new key, unset the environment variable or update it.")
	}

	cfg.APIKey = key
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	dbPath, err := config.DBPath()
	if err == nil {
		if store, err := cache.Open(dbPath); err == nil {
			defer store.Close()
			_ = store.SetAuthStatus(cfg.APIKeyHash(), "ok")
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

