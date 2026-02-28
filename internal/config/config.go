package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	EnvAPIKey    = "SUPERMODEL_API_KEY"
	EnvMode      = "UNCOMPACT_MODE"
	APIBaseURL   = "https://api.supermodeltools.com"
	DashboardURL = "https://dashboard.supermodeltools.com"

	ModeLocal = "local"
	ModeAPI   = "api"
)

// Config holds the Uncompact configuration.
type Config struct {
	APIKey    string `json:"api_key"`
	BaseURL   string `json:"base_url,omitempty"`
	MaxTokens int    `json:"max_tokens,omitempty"`
	Mode      string `json:"mode,omitempty"` // "local" or "api"; empty = auto-detect
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
	}

	// Apply defaults for any fields not set by file/env/flag.
	if cfg.BaseURL == "" {
		cfg.BaseURL = APIBaseURL
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 2000
	}

	return cfg, nil
}

// Save writes the config to disk.
func Save(cfg *Config) error {
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
	return os.WriteFile(cfgFile, data, 0600)
}

// IsAuthenticated returns true if an API key is configured.
func (c *Config) IsAuthenticated() bool {
	return c.APIKey != ""
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
