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
// PATH is prepended explicitly because Claude Code hooks run with a restricted environment
// that typically does not include ~/go/bin or other user-specific binary directories.
var uncompactHooks = map[string][]Hook{
	"Stop": {
		{
			Hooks: []Command{
				{Type: "command", Command: `bash -c 'export PATH="$HOME/go/bin:$HOME/.local/bin:/usr/local/bin:/opt/homebrew/bin:$PATH"; uncompact run --post-compact'`},
			},
		},
	},
	"UserPromptSubmit": {
		{
			Hooks: []Command{
				{Type: "command", Command: `bash -c 'export PATH="$HOME/go/bin:$HOME/.local/bin:/usr/local/bin:/opt/homebrew/bin:$PATH"; uncompact show-cache'`},
			},
		},
		{
			Hooks: []Command{
				{Type: "command", Command: `bash -c 'export PATH="$HOME/go/bin:$HOME/.local/bin:/usr/local/bin:/opt/homebrew/bin:$PATH"; uncompact pregen &'`},
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
			return nil, fmt.Errorf("invalid settings.json at %s: %w", settingsPath, err)
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
	oldHooksJSON, _ := json.MarshalIndent(existingHooks, "", "  ")
	newHooksJSON, _ := json.MarshalIndent(merged, "", "  ")
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
	if err := os.WriteFile(settingsPath, finalJSON, 0600); err != nil {
		return nil, fmt.Errorf("writing settings.json: %w", err)
	}

	return result, nil
}

// commandExistsInHooks reports whether any command in hookList contains one of
// the given substrings. Used to detect both direct ("uncompact run") and
// wrapper-script ("uncompact-hook.sh") forms of the same logical hook.
func commandExistsInHooks(hookList []Hook, matches ...string) bool {
	for _, h := range hookList {
		for _, cmd := range h.Hooks {
			for _, match := range matches {
				if strings.Contains(cmd.Command, match) {
					return true
				}
			}
		}
	}
	return false
}

// isAlreadyInstalled checks if ALL uncompact hooks are present.
func isAlreadyInstalled(hooks map[string][]Hook) bool {
	return commandExistsInHooks(hooks["Stop"], "uncompact run", "uncompact-hook.sh") &&
		commandExistsInHooks(hooks["UserPromptSubmit"], "uncompact show-cache", "show-hook.sh") &&
		commandExistsInHooks(hooks["UserPromptSubmit"], "uncompact pregen")
}

// mergeHooks adds hooks from toAdd into existing, skipping any whose commands
// are already present. Safe to call repeatedly — idempotent.
func mergeHooks(existing, toAdd map[string][]Hook) map[string][]Hook {
	result := make(map[string][]Hook)
	for k, v := range existing {
		result[k] = v
	}
	for event, hooks := range toAdd {
		for _, hook := range hooks {
			skip := false
			for _, cmd := range hook.Hooks {
				matches := []string{cmd.Command}
				switch event {
				case "Stop":
					matches = append(matches, "uncompact run", "uncompact-hook.sh")
				case "UserPromptSubmit":
					if strings.Contains(cmd.Command, "show-cache") {
						matches = append(matches, "uncompact show-cache", "show-hook.sh")
					} else if strings.Contains(cmd.Command, "pregen") {
						matches = append(matches, "uncompact pregen")
					}
				}
				if commandExistsInHooks(result[event], matches...) {
					skip = true
					break
				}
			}
			if !skip {
				result[event] = append(result[event], hook)
			}
		}
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

	// LCS-based sequential diff so that repeated tokens like braces and
	// "type": "command" are correctly attributed to newly-added blocks.
	m, n := len(beforeLines), len(afterLines)
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if beforeLines[i-1] == afterLines[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}

	// Walk back through the LCS table to reconstruct the diff.
	type diffOp struct {
		added   bool
		removed bool
		line    string
	}
	ops := make([]diffOp, 0, m+n)
	i, j := m, n
	for i > 0 || j > 0 {
		switch {
		case i > 0 && j > 0 && beforeLines[i-1] == afterLines[j-1]:
			ops = append(ops, diffOp{line: beforeLines[i-1]})
			i--
			j--
		case j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]):
			ops = append(ops, diffOp{added: true, line: afterLines[j-1]})
			j--
		default:
			ops = append(ops, diffOp{removed: true, line: beforeLines[i-1]})
			i--
		}
	}

	// ops were accumulated in reverse order.
	for k, l := 0, len(ops)-1; k < l; k, l = k+1, l-1 {
		ops[k], ops[l] = ops[l], ops[k]
	}

	for _, o := range ops {
		if o.removed {
			sb.WriteString("- " + o.line + "\n")
		} else if o.added {
			sb.WriteString("+ " + o.line + "\n")
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
