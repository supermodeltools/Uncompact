package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	EnvAPIKey        = "SUPERMODEL_API_KEY"
	EnvMode          = "UNCOMPACT_MODE"
	EnvDashboardURL  = "UNCOMPACT_DASHBOARD_URL" // override base dashboard URL for staging
	APIBaseURL       = "https://api.supermodeltools.com"
	DashboardURL        = "https://dashboard.supermodeltools.com"
	DashboardKeyURL     = "https://dashboard.supermodeltools.com/api-keys/"
	DashboardCLIAuthURL = "https://dashboard.supermodeltools.com/cli-auth/"

	ModeLocal = "local"
	ModeAPI   = "api"

	DefaultMaxTokens = 2000
)

// Config holds the Uncompact configuration.
type Config struct {
	// APIKey is the decrypted API key. It is not stored in plain text.
	APIKey string `json:"-"`

	// SecureAPIKey is the encrypted API key stored in the config file.
	SecureAPIKey string `json:"api_key,omitempty"`

	BaseURL   string `json:"base_url,omitempty"`
	MaxTokens int    `json:"max_tokens,omitempty"`
	Mode      string `json:"mode,omitempty"` // "local" or "api"; empty = auto-detect

	// Source indicates where the API key was loaded from.
	Source string `json:"-"`
}

// EffectiveCLIAuthURL returns the dashboard CLI auth URL, allowing the base
// domain to be overridden via UNCOMPACT_DASHBOARD_URL for staging or local
// development. Example: UNCOMPACT_DASHBOARD_URL=https://staging.dashboard.supermodeltools.com
func EffectiveCLIAuthURL() string {
	if override := os.Getenv(EnvDashboardURL); override != "" {
		return strings.TrimRight(override, "/") + "/cli-auth/"
	}
	return DashboardCLIAuthURL
}

// ConfigDir returns the XDG-compatible config directory.
func ConfigDir() (string, error) {
	var base string
	switch runtime.GOOS {
	case "windows":
		base = os.Getenv("APPDATA")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(home, "AppData", "Roaming")
		}
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, "Library", "Application Support")
	default:
		xdg := os.Getenv("XDG_CONFIG_HOME")
		if xdg != "" {
			base = xdg
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(home, ".config")
		}
	}
	return filepath.Join(base, "uncompact"), nil
}

// ConfigFile returns the path to the config file.
func ConfigFile() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// DataDir returns the data directory for SQLite and other persistent data.
func DataDir() (string, error) {
	var base string
	switch runtime.GOOS {
	case "windows":
		base = os.Getenv("LOCALAPPDATA")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(home, "AppData", "Local")
		}
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, "Library", "Application Support")
	default:
		xdg := os.Getenv("XDG_DATA_HOME")
		if xdg != "" {
			base = xdg
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(home, ".local", "share")
		}
	}
	return filepath.Join(base, "uncompact"), nil
}

// DBPath returns the path to the SQLite database.
func DBPath() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "uncompact.db"), nil
}

// Load reads the config file, merging with environment variables and flag overrides.
// flagAPIKey takes precedence over env var, which takes precedence over config file.
func Load(flagAPIKey string) (*Config, error) {
	cfg := &Config{}

	// Load from config file
	cfgFile, err := ConfigFile()
	if err != nil {
		return nil, fmt.Errorf("resolving config file path: %w", err)
	}
	if data, err := os.ReadFile(cfgFile); err == nil {
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("malformed config file %s: %w", cfgFile, err)
		}

		// Decrypt or migrate the API key
		if cfg.SecureAPIKey != "" {
			if strings.HasPrefix(cfg.SecureAPIKey, "smsk_") {
				// Migration: existing plain text key found
				cfg.APIKey = cfg.SecureAPIKey
				cfg.Source = "config file (migrated to secure storage)"
			} else {
				// Normal case: decrypt the secure key
				decrypted, err := decrypt(cfg.SecureAPIKey)
				if err != nil {
					return nil, fmt.Errorf("decrypting API key from config: %w", err)
				}
				cfg.APIKey = decrypted
				cfg.Source = "config file"
			}
		}

		if cfg.Mode != "" {
			cfg.Mode = strings.ToLower(strings.TrimSpace(cfg.Mode))
			if err := ValidateMode(cfg.Mode); err != nil {
				return nil, fmt.Errorf("config file field \"mode\": %w", err)
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading config file %s: %w", cfgFile, err)
	}

	// Override with env var
	if envKey := os.Getenv(EnvAPIKey); envKey != "" {
		cfg.APIKey = envKey
		cfg.Source = "environment variable (" + EnvAPIKey + ")"
	}

	// Override mode with env var
	if envMode := os.Getenv(EnvMode); envMode != "" {
		normalized := strings.ToLower(strings.TrimSpace(envMode))
		if err := ValidateMode(normalized); err != nil {
			return nil, fmt.Errorf("%s: %w", EnvMode, err)
		}
		cfg.Mode = normalized
	}

	// Override with flag
	if flagAPIKey != "" {
		cfg.APIKey = flagAPIKey
		cfg.Source = "CLI flag"
	}

	// Apply defaults for any fields not set by file/env/flag.
	if cfg.BaseURL == "" {
		cfg.BaseURL = APIBaseURL
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = DefaultMaxTokens
	}

	return cfg, nil
}

// Save writes the config to disk.
func Save(cfg *Config) error {
	// Encrypt the API key before saving
	if cfg.APIKey != "" {
		encrypted, err := encrypt(cfg.APIKey)
		if err != nil {
			return fmt.Errorf("encrypting API key for storage: %w", err)
		}
		cfg.SecureAPIKey = encrypted
	} else {
		cfg.SecureAPIKey = ""
	}

	dir, err := ConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	cfgFile := filepath.Join(dir, "config.json")
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := cfgFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, cfgFile); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// IsAuthenticated returns true if an API key is configured.
func (c *Config) IsAuthenticated() bool {
	return c.APIKey != ""
}

// APIKeyHash returns a SHA-256 hash of the API key for secure caching.
func (c *Config) APIKeyHash() string {
	if c.APIKey == "" {
		return ""
	}
	hash := sha256.Sum256([]byte(c.APIKey))
	return hex.EncodeToString(hash[:])
}

// ValidateMode reports whether s is a recognised operation mode.
// An empty string is valid (triggers auto-detection in EffectiveMode).
func ValidateMode(s string) error {
	if s == "" || s == ModeLocal || s == ModeAPI {
		return nil
	}
	return fmt.Errorf("invalid mode %q: must be %q or %q", s, ModeLocal, ModeAPI)
}

// EffectiveMode returns the resolved operation mode, or an error if flagMode is
// not a recognised value. flagMode (from --mode flag) takes precedence over the
// config Mode field, which takes precedence over auto-detection.
// Auto-detection: defaults to ModeLocal when no API key is configured.
func (c *Config) EffectiveMode(flagMode string) (string, error) {
	mode := c.Mode
	if flagMode != "" {
		normalized := strings.ToLower(strings.TrimSpace(flagMode))
		if err := ValidateMode(normalized); err != nil {
			return "", fmt.Errorf("--mode flag: %w", err)
		}
		mode = normalized
	}
	switch mode {
	case ModeLocal, ModeAPI:
		return mode, nil
	}
	// Auto-detect: default to local if no API key configured
	if !c.IsAuthenticated() {
		return ModeLocal, nil
	}
	return ModeAPI, nil
}
