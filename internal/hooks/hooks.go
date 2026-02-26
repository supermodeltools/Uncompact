package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ClaudeSettings represents Claude Code's settings.json structure.
type ClaudeSettings struct {
	Hooks map[string][]Hook `json:"hooks,omitempty"`
	// Preserve other fields we don't know about
	Extra map[string]json.RawMessage `json:"-"`
}

// Hook represents a single hook definition.
type Hook struct {
	Matcher string    `json:"matcher,omitempty"`
	Hooks   []Command `json:"hooks"`
}

// Command is a command hook action.
type Command struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// uncompactHooks defines the hooks we inject.
var uncompactHooks = map[string][]Hook{
	"Stop": {
		{
			Matcher: "",
			Hooks: []Command{
				{Type: "command", Command: "uncompact run"},
			},
		},
	},
}

// FindSettingsFile returns the path to Claude Code's settings.json.
func FindSettingsFile() (string, error) {
	candidates := settingsCandidates()
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	// Return the first candidate as the default location to create
	if len(candidates) > 0 {
		return candidates[0], nil
	}
	return "", fmt.Errorf("could not determine Claude Code settings location")
}

// settingsCandidates returns candidate paths for settings.json.
func settingsCandidates() []string {
	var paths []string
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	switch runtime.GOOS {
	case "windows":
		appdata := os.Getenv("APPDATA")
		if appdata != "" {
			paths = append(paths, filepath.Join(appdata, "Claude", "settings.json"))
		}
		paths = append(paths, filepath.Join(home, ".claude", "settings.json"))
	case "darwin":
		paths = append(paths,
			filepath.Join(home, "Library", "Application Support", "Claude", "settings.json"),
			filepath.Join(home, ".claude", "settings.json"),
		)
	default:
		xdg := os.Getenv("XDG_CONFIG_HOME")
		if xdg != "" {
			paths = append(paths, filepath.Join(xdg, "Claude", "settings.json"))
		}
		paths = append(paths,
			filepath.Join(home, ".config", "Claude", "settings.json"),
			filepath.Join(home, ".claude", "settings.json"),
		)
	}
	return paths
}

// InstallResult holds the result of an install operation.
type InstallResult struct {
	SettingsPath string
	Diff         string
	AlreadySet   bool
}

// Install merges the Uncompact hooks into the Claude Code settings.json.
// It returns a diff string for user review.
func Install(settingsPath string, dryRun bool) (*InstallResult, error) {
	result := &InstallResult{SettingsPath: settingsPath}

	// Read existing settings
	var rawJSON map[string]json.RawMessage
	if data, err := os.ReadFile(settingsPath); err == nil {
		if err := json.Unmarshal(data, &rawJSON); err != nil {
			// File exists but is invalid JSON — warn and treat as empty
			fmt.Fprintf(os.Stderr, "Warning: existing settings.json has invalid JSON, will recreate hooks section\n")
			rawJSON = make(map[string]json.RawMessage)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading settings.json: %w", err)
	} else {
		rawJSON = make(map[string]json.RawMessage)
	}

	// Parse existing hooks section
	existingHooks := make(map[string][]Hook)
	if hooksRaw, ok := rawJSON["hooks"]; ok {
		if err := json.Unmarshal(hooksRaw, &existingHooks); err != nil {
			existingHooks = make(map[string][]Hook)
		}
	}

	// Check if already installed
	if isAlreadyInstalled(existingHooks) {
		result.AlreadySet = true
		result.Diff = "(no changes — Uncompact hooks already present)"
		return result, nil
	}

	// Merge our hooks
	merged := mergeHooks(existingHooks, uncompactHooks)

	// Build diff
	oldHooksJSON, _ := json.MarshalIndent(existingHooks, "  ", "  ")
	newHooksJSON, _ := json.MarshalIndent(merged, "  ", "  ")
	result.Diff = buildDiff(string(oldHooksJSON), string(newHooksJSON))

	if dryRun {
		return result, nil
	}

	// Write back
	newHooksRaw, err := json.Marshal(merged)
	if err != nil {
		return nil, err
	}
	rawJSON["hooks"] = newHooksRaw

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0755); err != nil {
		return nil, fmt.Errorf("creating settings directory: %w", err)
	}

	finalJSON, err := json.MarshalIndent(rawJSON, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(settingsPath, finalJSON, 0644); err != nil {
		return nil, fmt.Errorf("writing settings.json: %w", err)
	}

	return result, nil
}

// isAlreadyInstalled checks if uncompact hooks are already present.
func isAlreadyInstalled(hooks map[string][]Hook) bool {
	stopHooks, ok := hooks["Stop"]
	if !ok {
		return false
	}
	for _, h := range stopHooks {
		for _, cmd := range h.Hooks {
			if strings.Contains(cmd.Command, "uncompact run") {
				return true
			}
		}
	}
	return false
}

// mergeHooks merges new hooks into existing hooks without duplicating.
func mergeHooks(existing, toAdd map[string][]Hook) map[string][]Hook {
	result := make(map[string][]Hook)
	for k, v := range existing {
		result[k] = v
	}
	for event, hooks := range toAdd {
		result[event] = append(result[event], hooks...)
	}
	return result
}

// buildDiff creates a simple text diff between two JSON strings.
func buildDiff(before, after string) string {
	if before == after {
		return "(no changes)"
	}
	var sb strings.Builder
	sb.WriteString("--- hooks (before)\n")
	sb.WriteString("+++ hooks (after)\n")

	beforeLines := strings.Split(before, "\n")
	afterLines := strings.Split(after, "\n")

	// Simple line-by-line diff
	beforeSet := make(map[string]bool)
	for _, l := range beforeLines {
		beforeSet[l] = true
	}
	afterSet := make(map[string]bool)
	for _, l := range afterLines {
		afterSet[l] = true
	}

	for _, l := range beforeLines {
		if !afterSet[l] {
			sb.WriteString("- " + l + "\n")
		}
	}
	for _, l := range afterLines {
		if !beforeSet[l] {
			sb.WriteString("+ " + l + "\n")
		}
	}
	return sb.String()
}

// Verify checks if the Uncompact hooks are properly installed.
func Verify(settingsPath string) (bool, error) {
	data, err := os.ReadFile(settingsPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	var rawJSON map[string]json.RawMessage
	if err := json.Unmarshal(data, &rawJSON); err != nil {
		return false, fmt.Errorf("invalid settings.json: %w", err)
	}

	var hooks map[string][]Hook
	if hooksRaw, ok := rawJSON["hooks"]; ok {
		if err := json.Unmarshal(hooksRaw, &hooks); err != nil {
			return false, nil
		}
	}

	return isAlreadyInstalled(hooks), nil
}
