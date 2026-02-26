package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Config holds all Uncompact configuration.
type Config struct {
	APIKey      string `json:"api_key"`
	APIURL      string `json:"api_url,omitempty"`
	DBPath      string `json:"db_path,omitempty"`
	CacheTTLMin int    `json:"cache_ttl_minutes,omitempty"`
	MaxCacheMB  int    `json:"max_cache_mb,omitempty"`
}

const (
	defaultAPIURL      = "https://api.supermodeltools.com"
	defaultCacheTTLMin = 15
	defaultMaxCacheMB  = 100
)

// Default returns a Config with default values.
func Default() *Config {
	return &Config{
		APIURL:      defaultAPIURL,
		DBPath:      defaultDBPath(),
		CacheTTLMin: defaultCacheTTLMin,
		MaxCacheMB:  defaultMaxCacheMB,
	}
}

// Load reads the config from disk. Returns an error if not found.
func Load() (*Config, error) {
	// Check env var first
	if key := os.Getenv("SUPERMODEL_API_KEY"); key != "" {
		cfg := Default()
		cfg.APIKey = key
		if url := os.Getenv("SUPERMODEL_API_URL"); url != "" {
			cfg.APIURL = url
		}
		return cfg, nil
	}

	path := ConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config not found at %s: %w", path, err)
	}

	cfg := Default()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("invalid config at %s: %w", path, err)
	}

	return cfg, nil
}

// Save writes the config to disk.
func Save(cfg *Config) error {
	path := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}

// ConfigPath returns the path to the config file.
func ConfigPath() string {
	return filepath.Join(configDir(), "config.json")
}

func configDir() string {
	if dir := os.Getenv("UNCOMPACT_CONFIG_DIR"); dir != "" {
		return dir
	}

	// Follow XDG on Linux, Library on macOS, AppData on Windows
	switch runtime.GOOS {
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", "uncompact")
	case "windows":
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			return filepath.Join(appdata, "uncompact")
		}
	}

	// Linux / fallback: XDG_CONFIG_HOME or ~/.config
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "uncompact")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "uncompact")
}

func defaultDBPath() string {
	switch runtime.GOOS {
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", "uncompact", "cache.db")
	case "windows":
		if appdata := os.Getenv("LOCALAPPDATA"); appdata != "" {
			return filepath.Join(appdata, "uncompact", "cache.db")
		}
	}
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "uncompact", "cache.db")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "uncompact", "cache.db")
}
