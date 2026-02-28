package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// setConfigDir redirects config operations to a temp directory via XDG_CONFIG_HOME
// (Linux/default) so tests never touch the real user config. Returns the temp dir.
func setConfigDir(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	return tmpDir
}

// writeConfigFile writes a Config as JSON to the expected config path under tmpDir.
func writeConfigFile(t *testing.T, tmpDir string, cfg *Config) {
	t.Helper()
	cfgDir := filepath.Join(tmpDir, "uncompact")
	if err := os.MkdirAll(cfgDir, 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), data, 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// --- Load ---

func TestLoad_MissingConfigFileUsesDefaults(t *testing.T) {
	setConfigDir(t)
	t.Setenv(EnvAPIKey, "")
	t.Setenv(EnvMode, "")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load with missing config file: %v", err)
	}
	if cfg.MaxTokens != 2000 {
		t.Errorf("MaxTokens = %d, want 2000", cfg.MaxTokens)
	}
	if cfg.BaseURL != APIBaseURL {
		t.Errorf("BaseURL = %q, want %q", cfg.BaseURL, APIBaseURL)
	}
}

func TestLoad_DefaultMaxTokens(t *testing.T) {
	tmpDir := setConfigDir(t)
	t.Setenv(EnvAPIKey, "")
	t.Setenv(EnvMode, "")

	// Config file with no max_tokens — default should be applied.
	writeConfigFile(t, tmpDir, &Config{APIKey: "key"})

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MaxTokens != 2000 {
		t.Errorf("MaxTokens = %d, want 2000 (default)", cfg.MaxTokens)
	}
}

func TestLoad_MalformedConfigFile(t *testing.T) {
	tmpDir := setConfigDir(t)
	t.Setenv(EnvAPIKey, "")
	t.Setenv(EnvMode, "")

	cfgDir := filepath.Join(tmpDir, "uncompact")
	if err := os.MkdirAll(cfgDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte("{not valid json}"), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := Load("")
	if err == nil {
		t.Fatal("expected error for malformed config file, got nil")
	}
}

func TestLoad_EnvAPIKeyOverridesConfigFile(t *testing.T) {
	tmpDir := setConfigDir(t)
	t.Setenv(EnvAPIKey, "env-key-123")
	t.Setenv(EnvMode, "")

	writeConfigFile(t, tmpDir, &Config{APIKey: "file-key"})

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.APIKey != "env-key-123" {
		t.Errorf("APIKey = %q, want %q (env should override config file)", cfg.APIKey, "env-key-123")
	}
}

func TestLoad_FlagAPIKeyOverridesEnv(t *testing.T) {
	setConfigDir(t)
	t.Setenv(EnvAPIKey, "env-key-123")
	t.Setenv(EnvMode, "")

	cfg, err := Load("flag-key-456")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.APIKey != "flag-key-456" {
		t.Errorf("APIKey = %q, want %q (flag should override env)", cfg.APIKey, "flag-key-456")
	}
}

func TestLoad_FlagAPIKeyOverridesConfigFile(t *testing.T) {
	tmpDir := setConfigDir(t)
	t.Setenv(EnvAPIKey, "")
	t.Setenv(EnvMode, "")

	writeConfigFile(t, tmpDir, &Config{APIKey: "file-key"})

	cfg, err := Load("flag-key")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.APIKey != "flag-key" {
		t.Errorf("APIKey = %q, want %q (flag should override config file)", cfg.APIKey, "flag-key")
	}
}

func TestLoad_PrecedenceOrder(t *testing.T) {
	// flag > env > config file > defaults
	tmpDir := setConfigDir(t)
	t.Setenv(EnvAPIKey, "env-key")
	t.Setenv(EnvMode, "")

	writeConfigFile(t, tmpDir, &Config{APIKey: "file-key"})

	// Flag wins over both env and file.
	cfg, err := Load("flag-key")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.APIKey != "flag-key" {
		t.Errorf("APIKey = %q, want flag-key (flag must win)", cfg.APIKey)
	}

	// Without flag, env wins over file.
	cfg2, err := Load("")
	if err != nil {
		t.Fatalf("Load (no flag): %v", err)
	}
	if cfg2.APIKey != "env-key" {
		t.Errorf("APIKey = %q, want env-key (env must win over file)", cfg2.APIKey)
	}
}

func TestLoad_EnvModeLowercasedAndTrimmed(t *testing.T) {
	setConfigDir(t)
	t.Setenv(EnvAPIKey, "")
	t.Setenv(EnvMode, "  API  ")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Mode != "api" {
		t.Errorf("Mode = %q, want %q (must be lower-cased and trimmed)", cfg.Mode, "api")
	}
}

// --- Save / round-trip ---

func TestSave_RoundTrip(t *testing.T) {
	setConfigDir(t)
	t.Setenv(EnvAPIKey, "")
	t.Setenv(EnvMode, "")

	orig := &Config{
		APIKey:    "roundtrip-key",
		BaseURL:   "https://example.com",
		MaxTokens: 4000,
		Mode:      ModeAPI,
	}
	if err := Save(orig); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load("")
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if loaded.APIKey != orig.APIKey {
		t.Errorf("APIKey = %q, want %q", loaded.APIKey, orig.APIKey)
	}
	if loaded.BaseURL != orig.BaseURL {
		t.Errorf("BaseURL = %q, want %q", loaded.BaseURL, orig.BaseURL)
	}
	if loaded.MaxTokens != orig.MaxTokens {
		t.Errorf("MaxTokens = %d, want %d", loaded.MaxTokens, orig.MaxTokens)
	}
	if loaded.Mode != orig.Mode {
		t.Errorf("Mode = %q, want %q", loaded.Mode, orig.Mode)
	}
}

func TestSave_FilePermissions(t *testing.T) {
	tmpDir := setConfigDir(t)
	t.Setenv(EnvAPIKey, "")
	t.Setenv(EnvMode, "")

	if err := Save(&Config{APIKey: "test-key", MaxTokens: 2000}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	cfgFile := filepath.Join(tmpDir, "uncompact", "config.json")
	info, err := os.Stat(cfgFile)
	if err != nil {
		t.Fatalf("Stat config file: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("file permissions = %04o, want 0600", info.Mode().Perm())
	}
}

// --- EffectiveMode ---

func TestEffectiveMode_ExplicitLocal(t *testing.T) {
	cfg := &Config{Mode: ModeLocal}
	if got := cfg.EffectiveMode(""); got != ModeLocal {
		t.Errorf("EffectiveMode() = %q, want %q", got, ModeLocal)
	}
}

func TestEffectiveMode_ExplicitAPI(t *testing.T) {
	cfg := &Config{Mode: ModeAPI, APIKey: "key"}
	if got := cfg.EffectiveMode(""); got != ModeAPI {
		t.Errorf("EffectiveMode() = %q, want %q", got, ModeAPI)
	}
}

func TestEffectiveMode_FlagOverridesConfigMode(t *testing.T) {
	cfg := &Config{Mode: ModeLocal, APIKey: "key"}
	if got := cfg.EffectiveMode(ModeAPI); got != ModeAPI {
		t.Errorf("EffectiveMode(%q) = %q, want %q", ModeAPI, got, ModeAPI)
	}
}

func TestEffectiveMode_AutoDetect_NoAPIKey(t *testing.T) {
	cfg := &Config{} // no APIKey, no Mode
	if got := cfg.EffectiveMode(""); got != ModeLocal {
		t.Errorf("EffectiveMode() with no API key = %q, want %q", got, ModeLocal)
	}
}

func TestEffectiveMode_AutoDetect_WithAPIKey(t *testing.T) {
	cfg := &Config{APIKey: "some-key"} // no explicit Mode
	if got := cfg.EffectiveMode(""); got != ModeAPI {
		t.Errorf("EffectiveMode() with API key = %q, want %q", got, ModeAPI)
	}
}

func TestEffectiveMode_FlagNormalizedBeforeMatch(t *testing.T) {
	cfg := &Config{}
	if got := cfg.EffectiveMode("  LOCAL  "); got != ModeLocal {
		t.Errorf("EffectiveMode(\"  LOCAL  \") = %q, want %q", got, ModeLocal)
	}
}
