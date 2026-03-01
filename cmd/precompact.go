package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/supermodeltools/uncompact/internal/activitylog"
	"github.com/supermodeltools/uncompact/internal/project"
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

	gitCtx, gitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer gitCancel()

	proj, err := project.Detect(gitCtx, "")
	if err != nil {
		logFn("[warn] pre-compact: project detection failed: %v", err)
		return silentExit()
	}

	content := buildSnapshotContent(input.TranscriptPath, logFn)

	snap := &snapshot.SessionSnapshot{
		Timestamp: time.Now().UTC(),
		Content:   content,
	}
	if err := snapshot.Write(proj.RootDir, snap); err != nil {
		logFn("[warn] pre-compact: writing snapshot: %v", err)
		return silentExit()
	}

	// Write activity log entry (non-fatal on error).
	_ = activitylog.Append(activitylog.Entry{
		EventType: activitylog.EventPreCompact,
		Timestamp: time.Now().UTC(),
		Project:   proj.RootDir,
	})

	logFn("[debug] pre-compact: snapshot written to %s", snapshot.Path(proj.RootDir))
	return nil
}

// buildSnapshotContent reads the transcript JSONL and extracts a human-readable
// session summary. Falls back to a minimal placeholder if parsing fails.
func buildSnapshotContent(transcriptPath string, logFn func(string, ...interface{})) string {
	if transcriptPath == "" {
		return minimalSnapshot()
	}

	f, err := os.Open(transcriptPath)
	if err != nil {
		logFn("[warn] pre-compact: reading transcript %s: %v", transcriptPath, err)
		return minimalSnapshot()
	}
	defer f.Close()

	const maxFilesInFocusBuffer = 200

	var userMessages []string
	var filesInFocus []string

	reader := bufio.NewReaderSize(f, 4*1024*1024)
	for {
		lineBytes, isPrefix, err := reader.ReadLine()
		if err == io.EOF {
			break
		}
		if err != nil {
			logFn("[warn] pre-compact: reading transcript: %v", err)
			break
		}
		if isPrefix {
			// Line exceeds 4 MB buffer; skip it and drain the remainder so
			// subsequent lines can still be processed.
			logFn("[warn] pre-compact: skipping oversized transcript line (>4MB)")
			for isPrefix {
				_, isPrefix, err = reader.ReadLine()
				if err != nil {
					break
				}
			}
			if err != nil && err != io.EOF {
				logFn("[warn] pre-compact: reading transcript after oversized line: %v", err)
				break
			}
			if err == io.EOF {
				break
			}
			continue
		}

		line := strings.TrimSpace(string(lineBytes))
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

		// Extract file paths from any message, appending each occurrence so
		// that re-referenced paths move toward the end of the slice.
		// Cap the buffer to avoid unbounded growth on long transcripts; evict
		// from the front to preserve "most recent references" semantics.
		for _, word := range strings.Fields(text) {
			word = strings.Trim(word, "`,\"'()[]")
			if looksLikeFilePath(word) {
				filesInFocus = append(filesInFocus, word)
				if len(filesInFocus) > maxFilesInFocusBuffer {
					filesInFocus = filesInFocus[len(filesInFocus)-maxFilesInFocusBuffer:]
				}
			}
		}
	}

	// Keep the 5 most recent user messages for context
	if len(userMessages) > 5 {
		userMessages = userMessages[len(userMessages)-5:]
	}

	// Deduplicate filesInFocus keeping only the last occurrence of each path,
	// so that files re-referenced later in the session stay near the end.
	{
		seen := make(map[string]bool)
		deduped := make([]string, 0, len(filesInFocus))
		for i := len(filesInFocus) - 1; i >= 0; i-- {
			if !seen[filesInFocus[i]] {
				seen[filesInFocus[i]] = true
				deduped = append(deduped, filesInFocus[i])
			}
		}
		// Reverse to restore chronological order (most-recently referenced last).
		for i, j := 0, len(deduped)-1; i < j; i, j = i+1, j-1 {
			deduped[i], deduped[j] = deduped[j], deduped[i]
		}
		filesInFocus = deduped
	}

	// Keep the 10 most recent file paths (mirrors the userMessages strategy above)
	if len(filesInFocus) > 10 {
		filesInFocus = filesInFocus[len(filesInFocus)-10:]
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
			runes := []rune(m)
			if len(runes) > 200 {
				m = string(runes[:197]) + "..."
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
				// Text block
				if t, ok := m["text"].(string); ok && t != "" {
					parts = append(parts, t)
				}
				// Tool-use block — include file_path so it is picked up by looksLikeFilePath
				if m["type"] == "tool_use" {
					if input, ok := m["input"].(map[string]interface{}); ok {
						if fp, ok := input["file_path"].(string); ok && fp != "" {
							parts = append(parts, fp)
						}
					}
				}
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

// knownSourceExts is the set of file extensions recognised as local source files.
var knownSourceExts = map[string]bool{
	".go": true, ".py": true, ".ts": true, ".tsx": true, ".js": true,
	".jsx": true, ".rs": true, ".md": true, ".json": true, ".yaml": true,
	".yml": true, ".toml": true, ".rb": true, ".java": true, ".c": true,
	".cpp": true, ".h": true, ".sh": true, ".bash": true, ".zsh": true,
	".txt": true, ".csv": true, ".html": true, ".css": true, ".scss": true,
	".proto": true, ".sql": true, ".tf": true, ".lock": true,
}

// hasKnownExt returns true if s ends with a recognised source-file extension.
func hasKnownExt(s string) bool {
	lastSeg := s[strings.LastIndex(s, "/")+1:]
	dotIdx := strings.LastIndex(lastSeg, ".")
	if dotIdx < 0 {
		return false
	}
	return knownSourceExts[lastSeg[dotIdx:]]
}

// looksLikeFilePath is a heuristic for detecting local file path tokens.
// It requires either an explicit relative/absolute path prefix, or a path
// whose final segment carries a recognised source-file extension.  This
// avoids false positives from Go import paths, domain names, and version
// strings that also contain "/" and ".".
func looksLikeFilePath(s string) bool {
	if len(s) < 3 || len(s) > 200 {
		return false
	}
	// Reject URL schemes so that https://... etc. are not treated as file paths.
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "ftp://") {
		return false
	}
	// Explicit local path prefix — require a recognised extension so that
	// /dev/null, /etc/hosts, /usr/bin/python, shell redirects, etc. are excluded.
	if strings.HasPrefix(s, "/") || strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") {
		return hasKnownExt(s)
	}
	// Windows absolute path: C:\ or C:/
	if len(s) >= 3 && s[1] == ':' && (s[2] == '/' || s[2] == '\\') {
		return hasKnownExt(s)
	}
	// For bare paths (no leading slash/dot) require the last path segment to
	// carry a recognised source-file extension so that import paths such as
	// "github.com/foo/bar" are not mistaken for file paths.
	if !strings.Contains(s, "/") {
		return false
	}
	// Reject domain-prefixed paths: "github.com/...", "golang.org/...", etc.
	// Local paths never have a dot in the first path segment.
	firstSeg := s[:strings.Index(s, "/")]
	if strings.Contains(firstSeg, ".") {
		return false
	}
	return hasKnownExt(s)
}
