package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/supermodeltools/uncompact/internal/snapshot"
)

var preCompactCmd = &cobra.Command{
	Use:   "pre-compact",
	Short: "Capture session state before Claude Code compaction (used by the PreCompact hook)",
	Long: `pre-compact reads the conversation transcript before compaction occurs and
writes a Markdown session snapshot to .uncompact/session-snapshot.md.

This command is invoked by the Claude Code PreCompact hook. The snapshot is
read back by 'uncompact run --post-compact' after compaction and injected into
the context bomb, restoring session awareness alongside project structure.`,
	RunE:          preCompactHandler,
	SilenceErrors: true,
	SilenceUsage:  true,
}

func init() {
	rootCmd.AddCommand(preCompactCmd)
}

// preCompactInput is the JSON payload sent by Claude Code on stdin for the PreCompact hook.
type preCompactInput struct {
	HookEventName  string `json:"hook_event_name"`
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
}

// transcriptEntry is a single line in the Claude Code conversation transcript JSONL.
type transcriptEntry struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // string or []contentBlock
}

// contentBlock is one element of a structured content array.
type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func preCompactHandler(cmd *cobra.Command, args []string) error {
	logFn := makeLogger()

	stdinData, err := io.ReadAll(os.Stdin)
	if err != nil {
		logFn("[warn] pre-compact: reading stdin: %v", err)
		return silentExit()
	}

	var input preCompactInput
	if err := json.Unmarshal(stdinData, &input); err != nil {
		logFn("[warn] pre-compact: parsing stdin JSON: %v", err)
		return silentExit()
	}

	cwd, err := os.Getwd()
	if err != nil {
		logFn("[warn] pre-compact: getwd: %v", err)
		return silentExit()
	}

	content := buildSnapshotContent(input.TranscriptPath, logFn)

	snap := &snapshot.SessionSnapshot{
		Timestamp: time.Now().UTC(),
		Content:   content,
	}
	if err := snapshot.Write(cwd, snap); err != nil {
		logFn("[warn] pre-compact: writing snapshot: %v", err)
		return silentExit()
	}

	logFn("[debug] pre-compact: snapshot written to %s", snapshot.Path(cwd))
	return nil
}

// buildSnapshotContent reads the transcript JSONL and extracts a human-readable
// session summary. Falls back to a minimal placeholder if parsing fails.
func buildSnapshotContent(transcriptPath string, logFn func(string, ...interface{})) string {
	if transcriptPath == "" {
		return minimalSnapshot()
	}

	data, err := os.ReadFile(transcriptPath)
	if err != nil {
		logFn("[warn] pre-compact: reading transcript %s: %v", transcriptPath, err)
		return minimalSnapshot()
	}

	var userMessages []string
	var filesInFocus []string
	seenFiles := make(map[string]bool)

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var entry transcriptEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}

		text := extractMessageText(entry.Content)
		if text == "" {
			continue
		}

		if entry.Role == "user" {
			userMessages = append(userMessages, text)
		}

		// Extract file paths from any message
		for _, word := range strings.Fields(text) {
			word = strings.Trim(word, "`,\"'()[]")
			if looksLikeFilePath(word) && !seenFiles[word] {
				seenFiles[word] = true
				filesInFocus = append(filesInFocus, word)
				if len(filesInFocus) >= 10 {
					break
				}
			}
		}
	}

	// Keep the 5 most recent user messages for context
	if len(userMessages) > 5 {
		userMessages = userMessages[len(userMessages)-5:]
	}

	return buildSnapshotMarkdown(userMessages, filesInFocus)
}

func buildSnapshotMarkdown(userMessages, filesInFocus []string) string {
	var sb strings.Builder
	sb.WriteString("## Session State (before compaction)\n\n")

	if len(userMessages) > 0 {
		sb.WriteString("**Recent context:**\n")
		for _, m := range userMessages {
			// Flatten to single line and truncate
			m = strings.Join(strings.Fields(m), " ")
			if len(m) > 200 {
				m = m[:197] + "..."
			}
			sb.WriteString(fmt.Sprintf("- %s\n", m))
		}
		sb.WriteString("\n")
	}

	if len(filesInFocus) > 0 {
		sb.WriteString("**Files in focus:**\n")
		for _, f := range filesInFocus {
			sb.WriteString(fmt.Sprintf("- %s\n", f))
		}
		sb.WriteString("\n")
	}

	if len(userMessages) == 0 && len(filesInFocus) == 0 {
		sb.WriteString("*(Session state captured before compaction)*\n")
	}

	return sb.String()
}

// minimalSnapshot returns a placeholder when transcript parsing is not possible.
func minimalSnapshot() string {
	return "## Session State (before compaction)\n\n*(Session state captured before compaction)*\n"
}

// extractMessageText returns the plain text from a message content field,
// which may be a string or a list of content blocks.
func extractMessageText(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var parts []string
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				if t, ok := m["text"].(string); ok && t != "" {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

// looksLikeFilePath is a simple heuristic for detecting file path tokens.
func looksLikeFilePath(s string) bool {
	if len(s) < 3 || len(s) > 200 {
		return false
	}
	// Must contain a slash and a dot to look like a file path
	return strings.Contains(s, "/") && strings.Contains(s, ".")
}
