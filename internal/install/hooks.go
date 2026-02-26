package install

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// HooksConfig is the shape of Claude Code's settings.json hooks section.
type HooksConfig struct {
	Hooks map[string][]HookMatcher `json:"hooks,omitempty"`
}

// HookMatcher matches events and triggers hook commands.
type HookMatcher struct {
	Matcher string     `json:"matcher,omitempty"`
	Hooks   []HookItem `json:"hooks"`
}

// HookItem is a single hook command.
type HookItem struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// settingsJSON is the full Claude Code settings.json shape we care about.
type settingsJSON struct {
	Hooks map[string][]HookMatcher `json:"hooks,omitempty"`
	// Preserve unknown fields
	Extra map[string]json.RawMessage `json:"-"`
}

// uncompactHooks defines the hooks Uncompact needs.
var uncompactHooks = map[string]HookMatcher{
	"Stop": {
		// Triggers after compaction via the Stop event
		Hooks: []HookItem{
			{Type: "command", Command: "uncompact run"},
		},
	},
	"UserPromptSubmit": {
		// Detects session start: matcher looks for first prompt of session
		Matcher: "session_start",
		Hooks: []HookItem{
			{Type: "command", Command: "uncompact run --rate-limit 5"},
		},
	},
}

// FindSettingsJSON locates the Claude Code settings.json file.
func FindSettingsJSON() (string, error) {
	candidates := settingsJSONCandidates()
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	// Return the preferred location even if it doesn't exist
	if len(candidates) > 0 {
		return candidates[0], nil
	}
	return "", fmt.Errorf("could not determine settings.json location")
}

func settingsJSONCandidates() []string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return []string{
			filepath.Join(home, "Library", "Application Support", "Claude", "claude_code", "settings.json"),
			filepath.Join(home, ".claude", "settings.json"),
		}
	case "windows":
		appdata := os.Getenv("APPDATA")
		return []string{
			filepath.Join(appdata, "Claude", "claude_code", "settings.json"),
			filepath.Join(home, ".claude", "settings.json"),
		}
	default: // Linux
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			return []string{
				filepath.Join(xdg, "claude", "settings.json"),
				filepath.Join(home, ".claude", "settings.json"),
			}
		}
		return []string{
			filepath.Join(home, ".config", "claude", "settings.json"),
			filepath.Join(home, ".claude", "settings.json"),
		}
	}
}

// InstallHooks merges Uncompact hooks into settings.json.
// If yes is false, it prints a diff and asks for confirmation.
func InstallHooks(yes bool) error {
	path, err := FindSettingsJSON()
	if err != nil {
		return err
	}

	existing, raw, err := loadSettings(path)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", path, err)
	}

	merged := mergeHooks(existing, uncompactHooks)

	newJSON, err := marshalSettings(merged, raw)
	if err != nil {
		return fmt.Errorf("failed to serialize settings: %w", err)
	}

	diff := diffSettings(raw, newJSON)
	if diff == "" {
		fmt.Println("✓ Uncompact hooks are already installed")
		return nil
	}

	fmt.Println("Proposed changes to", path)
	fmt.Println()
	printDiff(diff)
	fmt.Println()

	if !yes {
		fmt.Print("Apply changes? [Y/n]: ")
		var answer string
		fmt.Scanln(&answer)
		if answer != "" && answer != "y" && answer != "Y" {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	if err := os.WriteFile(path, []byte(newJSON), 0644); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}

	fmt.Println("✓ Hooks installed to", path)
	fmt.Println()
	fmt.Println("Restart Claude Code for the hooks to take effect.")
	return nil
}

// VerifyHooks checks if Uncompact hooks are correctly configured.
// Returns a list of issues (empty means all good).
func VerifyHooks() ([]string, error) {
	path, err := FindSettingsJSON()
	if err != nil {
		return []string{"settings.json not found"}, nil
	}

	existing, _, err := loadSettings(path)
	if err != nil {
		return []string{fmt.Sprintf("cannot read %s: %v", path, err)}, nil
	}

	var issues []string
	for event, matcher := range uncompactHooks {
		if !hookPresent(existing, event, matcher.Hooks[0].Command) {
			issues = append(issues, fmt.Sprintf("hook for %s event not found (command: %s)", event, matcher.Hooks[0].Command))
		}
	}
	return issues, nil
}

func loadSettings(path string) (map[string][]HookMatcher, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string][]HookMatcher{}, "{}", nil
		}
		return nil, "", err
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, string(data), fmt.Errorf("invalid JSON: %w", err)
	}

	hooks := make(map[string][]HookMatcher)
	if hooksRaw, ok := raw["hooks"]; ok {
		if err := json.Unmarshal(hooksRaw, &hooks); err != nil {
			return nil, string(data), fmt.Errorf("invalid hooks config: %w", err)
		}
	}

	return hooks, string(data), nil
}

func mergeHooks(existing map[string][]HookMatcher, newHooks map[string]HookMatcher) map[string][]HookMatcher {
	result := make(map[string][]HookMatcher)
	for k, v := range existing {
		result[k] = v
	}

	for event, matcher := range newHooks {
		// Check if our command is already there
		if hookPresent(existing, event, matcher.Hooks[0].Command) {
			continue
		}
		result[event] = append(result[event], matcher)
	}

	return result
}

func hookPresent(hooks map[string][]HookMatcher, event, command string) bool {
	for _, matcher := range hooks[event] {
		for _, h := range matcher.Hooks {
			if h.Command == command {
				return true
			}
		}
	}
	return false
}

func marshalSettings(hooks map[string][]HookMatcher, originalRaw string) (string, error) {
	var raw map[string]json.RawMessage
	if originalRaw != "" && originalRaw != "{}" {
		if err := json.Unmarshal([]byte(originalRaw), &raw); err != nil {
			raw = make(map[string]json.RawMessage)
		}
	} else {
		raw = make(map[string]json.RawMessage)
	}

	hooksJSON, err := json.MarshalIndent(hooks, "", "  ")
	if err != nil {
		return "", err
	}
	raw["hooks"] = hooksJSON

	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out) + "\n", nil
}

func diffSettings(oldJSON, newJSON string) string {
	if oldJSON == newJSON {
		return ""
	}
	oldLines := strings.Split(oldJSON, "\n")
	newLines := strings.Split(newJSON, "\n")

	// Simple line diff
	var diff strings.Builder
	oldSet := make(map[string]bool)
	newSet := make(map[string]bool)
	for _, l := range oldLines {
		oldSet[l] = true
	}
	for _, l := range newLines {
		newSet[l] = true
	}

	for _, l := range oldLines {
		if !newSet[l] {
			diff.WriteString("- " + l + "\n")
		}
	}
	for _, l := range newLines {
		if !oldSet[l] {
			diff.WriteString("+ " + l + "\n")
		}
	}
	return diff.String()
}

func printDiff(diff string) {
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "+") {
			fmt.Println(line)
		} else if strings.HasPrefix(line, "-") {
			fmt.Println(line)
		} else {
			fmt.Println(line)
		}
	}
}
