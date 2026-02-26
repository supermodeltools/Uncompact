package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	DefaultAPIBase    = "https://api.supermodeltools.com"
	DefaultTTLSeconds = 900 // 15 minutes
	DefaultMaxTokens  = 2000
	DashboardURL      = "https://dashboard.supermodeltools.com"
)

// Config holds Uncompact configuration persisted to disk.
type Config struct {
	APIKey  string `json:"api_key,omitempty"`
	APIBase string `json:"api_base,omitempty"`
}

// ConfigDir returns the platform config directory for uncompact.
func ConfigDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("could not determine config dir: %w", err)
	}
	return filepath.Join(base, "uncompact"), nil
}

// ConfigPath returns the full path to the config file.
func ConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Load reads config from disk, returning defaults if the file does not exist.
func Load() (*Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return &Config{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return &cfg, nil
}

// Save writes config to disk, creating the directory if needed.
func Save(cfg *Config) error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	path := filepath.Join(dir, "config.json")
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("serializing config: %w", err)
	}
	return os.WriteFile(path, append(data, '\n'), 0600)
}

// ResolveAPIKey returns the API key from (in priority order):
// 1. The provided override
// 2. SUPERMODEL_API_KEY env var
// 3. Config file
func ResolveAPIKey(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	if env := os.Getenv("SUPERMODEL_API_KEY"); env != "" {
		return env, nil
	}
	cfg, err := Load()
	if err != nil {
		return "", err
	}
	if cfg.APIKey != "" {
		return cfg.APIKey, nil
	}
	return "", fmt.Errorf("no API key configured — run `uncompact auth login` or set SUPERMODEL_API_KEY")
}

// ResolveAPIBase returns the API base URL, falling back to the default.
func ResolveAPIBase(cfg *Config) string {
	if cfg != nil && cfg.APIBase != "" {
		return cfg.APIBase
	}
	if env := os.Getenv("SUPERMODEL_API_BASE"); env != "" {
		return env
	}
	return DefaultAPIBase
}

// CacheDir returns the platform cache directory for uncompact.
func CacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("could not determine cache dir: %w", err)
	}
	return filepath.Join(base, "uncompact"), nil
}
