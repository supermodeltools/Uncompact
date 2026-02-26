package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// HookDef is a single hook entry in settings.json.
type HookDef struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// HookMatcher is a matcher+hooks pair.
type HookMatcher struct {
	Matcher string    `json:"matcher"`
	Hooks   []HookDef `json:"hooks"`
}

// SettingsHooks is the hooks section of settings.json.
type SettingsHooks map[string][]HookMatcher

// Settings is a partial representation of Claude Code settings.json.
type Settings struct {
	Hooks SettingsHooks `json:"hooks,omitempty"`
	// Other fields are preserved via raw JSON
}

const (
	// PostCompact is the Claude Code hook event for after compaction.
	PostCompact = "PostCompact"
	// SessionStart fires when a new session begins.
	SessionStart = "SessionStart"
)

// DefaultHooks returns the recommended Uncompact hook configuration.
func DefaultHooks(maxTokens int) SettingsHooks {
	cmd := fmt.Sprintf("uncompact run --max-tokens %d", maxTokens)
	return SettingsHooks{
		PostCompact: {
			{
				Matcher: ".*",
				Hooks: []HookDef{
					{Type: "command", Command: cmd},
				},
			},
		},
	}
}

// SettingsPath returns the Claude Code settings.json path for the current platform.
func SettingsPath() (string, error) {
	switch runtime.GOOS {
	case "windows":
		base := os.Getenv("APPDATA")
		if base == "" {
			return "", fmt.Errorf("APPDATA not set")
		}
		return filepath.Join(base, "Claude", "claude_code", "settings.json"), nil
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", "Claude", "claude_code", "settings.json"), nil
	default:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".config", "claude", "claude_code", "settings.json"), nil
	}
}

// InstallResult describes the outcome of an install operation.
type InstallResult struct {
	SettingsPath string
	Diff         string
	WroteFile    bool
}

// Install merges Uncompact hooks into Claude Code settings.json.
// It returns a diff-like description before writing, and only writes if dryRun is false.
func Install(maxTokens int, dryRun bool) (*InstallResult, error) {
	path, err := SettingsPath()
	if err != nil {
		return nil, fmt.Errorf("locating settings.json: %w", err)
	}

	// Read existing settings
	var raw map[string]json.RawMessage
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading settings.json: %w", err)
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("parsing settings.json: %w", err)
		}
	} else {
		raw = make(map[string]json.RawMessage)
	}

	// Merge hooks
	wantHooks := DefaultHooks(maxTokens)
	var existingHooks SettingsHooks
	if hooksRaw, ok := raw["hooks"]; ok {
		_ = json.Unmarshal(hooksRaw, &existingHooks)
	}
	if existingHooks == nil {
		existingHooks = make(SettingsHooks)
	}

	var diff string
	for event, matchers := range wantHooks {
		if _, exists := existingHooks[event]; !exists {
			existingHooks[event] = matchers
			diff += fmt.Sprintf("+ hooks.%s: added uncompact %s hook\n", event, event)
		} else {
			diff += fmt.Sprintf("~ hooks.%s: already configured (skipped)\n", event)
		}
	}

	if diff == "" {
		diff = "No changes needed — Uncompact hooks are already installed."
	}

	result := &InstallResult{
		SettingsPath: path,
		Diff:         diff,
	}

	if !dryRun {
		mergedHooks, err := json.Marshal(existingHooks)
		if err != nil {
			return nil, fmt.Errorf("serializing hooks: %w", err)
		}
		raw["hooks"] = json.RawMessage(mergedHooks)

		out, err := json.MarshalIndent(raw, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("serializing settings: %w", err)
		}

		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return nil, fmt.Errorf("creating settings dir: %w", err)
		}
		if err := os.WriteFile(path, append(out, '\n'), 0644); err != nil {
			return nil, fmt.Errorf("writing settings.json: %w", err)
		}
		result.WroteFile = true
	}

	return result, nil
}
