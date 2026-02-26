package install

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// HooksConfig is the Uncompact hooks block to merge into settings.json.
var HooksConfig = map[string]interface{}{
	"hooks": map[string]interface{}{
		"PostToolUse": []map[string]interface{}{
			{
				"matcher": "Bash",
				"hooks": []map[string]interface{}{
					{
						"type":    "command",
						"command": "uncompact run",
					},
				},
			},
		},
		"PreToolUse": []map[string]interface{}{},
	},
}

// Run merges Uncompact hooks into settings.json. If dryRun is true, it only prints
// the diff without writing.
func Run(dryRun bool) error {
	settingsPath, err := findSettingsJSON()
	if err != nil {
		return fmt.Errorf("could not locate Claude Code settings.json: %w\n\nInstall Claude Code first: https://claude.ai/code", err)
	}

	current, err := readJSON(settingsPath)
	if err != nil {
		// File may not exist yet; start fresh
		current = map[string]interface{}{}
	}

	merged := mergeHooks(current, HooksConfig)
	mergedBytes, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling merged config: %w", err)
	}

	fmt.Printf("Settings file: %s\n\n", settingsPath)
	fmt.Println("--- changes to be applied ---")
	printDiff(current, merged)

	if dryRun {
		fmt.Println("\n(dry-run: no changes written)")
		return nil
	}

	if err := os.WriteFile(settingsPath, append(mergedBytes, '\n'), 0600); err != nil {
		return fmt.Errorf("writing settings.json: %w", err)
	}

	fmt.Println("\n✓ Hooks installed successfully.")
	fmt.Println("  Uncompact will re-inject context on configured hook events.")
	return nil
}

// Init runs an interactive first-time setup wizard.
func Init() error {
	fmt.Println("Uncompact Setup Wizard")
	fmt.Println("======================")
	fmt.Println()
	fmt.Printf("1. Get your API key at: https://dashboard.supermodeltools.com\n")
	fmt.Printf("2. Set it as an environment variable:\n")
	fmt.Printf("   export SUPERMODEL_API_KEY=<your-key>\n")
	fmt.Println()
	fmt.Println("3. Installing hooks into Claude Code settings.json...")
	return Run(false)
}

// Verify checks whether the hooks are correctly installed. Returns true if ok,
// plus any issues found.
func Verify() (bool, []string) {
	settingsPath, err := findSettingsJSON()
	if err != nil {
		return false, []string{"could not locate settings.json: " + err.Error()}
	}

	settings, err := readJSON(settingsPath)
	if err != nil {
		return false, []string{"could not read settings.json: " + err.Error()}
	}

	var issues []string

	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		issues = append(issues, "hooks key missing from settings.json")
		return false, issues
	}

	postToolUse, ok := hooks["PostToolUse"].([]interface{})
	if !ok || len(postToolUse) == 0 {
		issues = append(issues, "PostToolUse hook not configured")
	}

	return len(issues) == 0, issues
}

func findSettingsJSON() (string, error) {
	var base string
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, "Library", "Application Support", "Claude")
	case "linux":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config", "claude")
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			return "", fmt.Errorf("APPDATA not set")
		}
		base = filepath.Join(appData, "Claude")
	default:
		return "", fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}

	path := filepath.Join(base, "settings.json")
	return path, nil
}

func readJSON(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// mergeHooks merges the uncompact hooks config into the existing settings,
// without clobbering existing hook entries.
func mergeHooks(existing, additions map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range existing {
		result[k] = v
	}
	for k, v := range additions {
		result[k] = v
	}
	return result
}

func printDiff(before, after map[string]interface{}) {
	beforeBytes, _ := json.MarshalIndent(before, "", "  ")
	afterBytes, _ := json.MarshalIndent(after, "", "  ")
	fmt.Printf("Before:\n%s\n\nAfter:\n%s\n", string(beforeBytes), string(afterBytes))
}
