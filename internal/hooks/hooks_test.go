package hooks

import (
	"testing"
)

// --- isAlreadyInstalled ---

func TestIsAlreadyInstalled_EmptyMap(t *testing.T) {
	if isAlreadyInstalled(map[string][]Hook{}) {
		t.Error("expected false for empty hooks map")
	}
}

func TestIsAlreadyInstalled_FullyInstalled(t *testing.T) {
	hooks := map[string][]Hook{
		"PreCompact": {
			{Hooks: []Command{{Type: "command", Command: "uncompact pre-compact"}}},
		},
		"Stop": {
			{Hooks: []Command{{Type: "command", Command: "uncompact run --post-compact"}}},
		},
		"UserPromptSubmit": {
			{Hooks: []Command{{Type: "command", Command: "uncompact show-cache"}}},
			{Hooks: []Command{{Type: "command", Command: "uncompact pregen &"}}},
		},
	}
	if !isAlreadyInstalled(hooks) {
		t.Error("expected true when all hooks are present")
	}
}

func TestIsAlreadyInstalled_LegacyWrapperScripts(t *testing.T) {
	// Old-style hook installations use shell wrapper scripts.
	hooks := map[string][]Hook{
		"PreCompact": {
			{Hooks: []Command{{Type: "command", Command: "uncompact pre-compact"}}},
		},
		"Stop": {
			{Hooks: []Command{{Type: "command", Command: "/path/to/uncompact-hook.sh"}}},
		},
		"UserPromptSubmit": {
			{Hooks: []Command{{Type: "command", Command: "show-hook.sh"}}},
			{Hooks: []Command{{Type: "command", Command: "uncompact pregen &"}}},
		},
	}
	if !isAlreadyInstalled(hooks) {
		t.Error("expected true when legacy wrapper-script hooks are present")
	}
}

func TestIsAlreadyInstalled_MissingStop(t *testing.T) {
	hooks := map[string][]Hook{
		"PreCompact": {
			{Hooks: []Command{{Type: "command", Command: "uncompact pre-compact"}}},
		},
		"UserPromptSubmit": {
			{Hooks: []Command{{Type: "command", Command: "uncompact show-cache"}}},
			{Hooks: []Command{{Type: "command", Command: "uncompact pregen"}}},
		},
	}
	if isAlreadyInstalled(hooks) {
		t.Error("expected false when Stop hook is missing")
	}
}

func TestIsAlreadyInstalled_MissingPregen(t *testing.T) {
	hooks := map[string][]Hook{
		"PreCompact": {
			{Hooks: []Command{{Type: "command", Command: "uncompact pre-compact"}}},
		},
		"Stop": {
			{Hooks: []Command{{Type: "command", Command: "uncompact run"}}},
		},
		"UserPromptSubmit": {
			{Hooks: []Command{{Type: "command", Command: "uncompact show-cache"}}},
			// pregen is absent
		},
	}
	if isAlreadyInstalled(hooks) {
		t.Error("expected false when pregen hook is missing")
	}
}

// --- mergeHooks ---

func TestMergeHooks_FreshInstall(t *testing.T) {
	result := mergeHooks(map[string][]Hook{}, uncompactHooks)
	if !isAlreadyInstalled(result) {
		t.Error("expected all hooks installed after merging into empty map")
	}
}

func TestMergeHooks_Idempotent(t *testing.T) {
	first := mergeHooks(map[string][]Hook{}, uncompactHooks)
	second := mergeHooks(first, uncompactHooks)

	for event, firstHooks := range first {
		if len(second[event]) != len(firstHooks) {
			t.Errorf("mergeHooks not idempotent for %q: first=%d, second=%d",
				event, len(firstHooks), len(second[event]))
		}
	}
}

func TestMergeHooks_PreservesExistingHooks(t *testing.T) {
	existing := map[string][]Hook{
		"PreCompact": {
			{Hooks: []Command{{Type: "command", Command: "my-custom-tool"}}},
		},
	}

	result := mergeHooks(existing, uncompactHooks)

	// Custom hook should still be present.
	found := false
	for _, h := range result["PreCompact"] {
		for _, cmd := range h.Hooks {
			if cmd.Command == "my-custom-tool" {
				found = true
			}
		}
	}
	if !found {
		t.Error("mergeHooks should preserve existing custom hooks")
	}

	// Uncompact hook should also be present.
	if !commandExistsInHooks(result["PreCompact"], "uncompact pre-compact") {
		t.Error("mergeHooks should add uncompact PreCompact hook alongside existing hooks")
	}
}

func TestMergeHooks_PartialInstall_AddsOnlyMissing(t *testing.T) {
	// Only the PreCompact hook is pre-installed.
	partial := map[string][]Hook{
		"PreCompact": uncompactHooks["PreCompact"],
	}

	result := mergeHooks(partial, uncompactHooks)

	if !isAlreadyInstalled(result) {
		t.Error("expected all hooks after merging partial install")
	}

	// PreCompact should not be duplicated.
	preCompactCount := 0
	for _, h := range result["PreCompact"] {
		for _, cmd := range h.Hooks {
			if commandExistsInHooks([]Hook{{Hooks: []Command{cmd}}}, "uncompact pre-compact") {
				preCompactCount++
			}
		}
	}
	if preCompactCount != 1 {
		t.Errorf("expected exactly 1 uncompact PreCompact command, got %d", preCompactCount)
	}
}

func TestMergeHooks_DoesNotModifyExisting(t *testing.T) {
	original := map[string][]Hook{
		"Stop": {
			{Hooks: []Command{{Type: "command", Command: "other-tool"}}},
		},
	}
	originalLen := len(original["Stop"])

	mergeHooks(original, uncompactHooks)

	// mergeHooks should not mutate its inputs.
	if len(original["Stop"]) != originalLen {
		t.Errorf("mergeHooks mutated the existing map: len before=%d, after=%d", originalLen, len(original["Stop"]))
	}
}
