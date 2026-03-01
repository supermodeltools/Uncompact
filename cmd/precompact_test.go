package cmd

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestLooksLikeFilePath_Positive(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"go source file", "cmd/run.go"},
		{"relative path with dot-slash", "./foo/bar.go"},
		{"absolute path", "/abs/path.go"},
		{"nested internal path", "internal/template/render.go"},
		{"json config", "config/settings.json"},
		{"yaml in subdirectory", "deploy/k8s/pod.yaml"},
		{"windows absolute path backslash", `C:\Users\foo\project\main.go`},
		{"windows absolute path forward slash", "C:/Users/foo/project/main.go"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !looksLikeFilePath(tc.input) {
				t.Errorf("looksLikeFilePath(%q) = false, want true", tc.input)
			}
		})
	}
}

func TestLooksLikeFilePath_Negative(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"plain word", "hello"},
		{"empty string", ""},
		{"too short (2 chars)", "ab"},
		{"https URL", "https://example.com/foo.js"},
		{"http URL", "http://example.com/path.go"},
		{"ftp URL", "ftp://files.example.com/archive.tar.gz"},
		{"version string", "1.2.3"},
		{"semver with v prefix", "v1.2.3"},
		{"no slash", "filename.go"},
		{"no dot", "path/without/extension"},
		{"go module import path with extension", "github.com/user/repo/pkg/server.go"},
		{"go module import path nested", "golang.org/x/net/http2/hpack/hpack.go"},
		{"go module import path no ext", "github.com/foo/bar"},
		{"golang.org module path", "golang.org/x/text/encoding/charmap.go"},
		{"too long (>200 chars)", func() string {
			s := "/"
			for i := 0; i < 200; i++ {
				s += "x"
			}
			return s + ".go"
		}()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if looksLikeFilePath(tc.input) {
				t.Errorf("looksLikeFilePath(%q) = true, want false", tc.input)
			}
		})
	}
}

// extractMessageText tests

func TestExtractMessageText_PlainString(t *testing.T) {
	got := extractMessageText("hello world")
	if got != "hello world" {
		t.Errorf("extractMessageText(%q) = %q, want %q", "hello world", got, "hello world")
	}
}

func TestExtractMessageText_ContentBlocksJoined(t *testing.T) {
	content := []interface{}{
		map[string]interface{}{"type": "text", "text": "hello"},
		map[string]interface{}{"type": "text", "text": "world"},
	}
	got := extractMessageText(content)
	want := "hello world"
	if got != want {
		t.Errorf("extractMessageText() = %q, want %q", got, want)
	}
}

func TestExtractMessageText_NonTextBlocksSkipped(t *testing.T) {
	content := []interface{}{
		map[string]interface{}{"type": "tool_use", "input": map[string]interface{}{"cmd": "ls"}},
		map[string]interface{}{"type": "text", "text": "hello"},
	}
	got := extractMessageText(content)
	want := "hello"
	if got != want {
		t.Errorf("extractMessageText() = %q, want %q", got, want)
	}
}

func TestExtractMessageText_ToolUseFilePaths(t *testing.T) {
	cases := []struct {
		name  string
		field string
		value string
	}{
		{"file_path field", "file_path", "cmd/run.go"},
		{"path field", "path", "internal/cache/store.go"},
		{"pattern field", "pattern", "**/*.go"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			content := []interface{}{
				map[string]interface{}{
					"type":  "tool_use",
					"input": map[string]interface{}{tc.field: tc.value},
				},
			}
			got := extractMessageText(content)
			if !strings.Contains(got, tc.value) {
				t.Errorf("extractMessageText() = %q, expected to contain %q", got, tc.value)
			}
		})
	}
}

// buildSnapshotMarkdown tests

func TestBuildSnapshotMarkdown_WithData(t *testing.T) {
	messages := []string{"first message", "second message"}
	files := []string{"cmd/precompact.go", "internal/snapshot/write.go"}
	got := buildSnapshotMarkdown(messages, files)

	for _, want := range []string{"**Recent context:**", "**Files in focus:**", "first message", "cmd/precompact.go"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in output:\n%s", want, got)
		}
	}
}

func TestBuildSnapshotMarkdown_EmptyFallback(t *testing.T) {
	got := buildSnapshotMarkdown(nil, nil)
	want := "*(Session state captured before compaction)*"
	if !strings.Contains(got, want) {
		t.Errorf("buildSnapshotMarkdown(nil, nil) = %q, expected to contain %q", got, want)
	}
}

// buildSnapshotContent tests

func writeJSONLFile(t *testing.T, lines []string) string {
	t.Helper()
	f, err := os.CreateTemp("", "transcript*.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(f.Name()) })
	for _, line := range lines {
		fmt.Fprintln(f, line)
	}
	f.Close()
	return f.Name()
}

func TestBuildSnapshotContent_Last5UserMessages(t *testing.T) {
	var lines []string
	for i := 1; i <= 8; i++ {
		lines = append(lines, fmt.Sprintf(`{"role":"user","content":"message %d"}`, i))
	}
	path := writeJSONLFile(t, lines)

	noop := func(string, ...interface{}) {}
	got := buildSnapshotContent(path, noop)

	// Last 5 messages (4–8) must be present
	for i := 4; i <= 8; i++ {
		want := fmt.Sprintf("message %d", i)
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in output:\n%s", want, got)
		}
	}
	// First 3 messages must be absent
	for i := 1; i <= 3; i++ {
		notWant := fmt.Sprintf("message %d", i)
		if strings.Contains(got, notWant) {
			t.Errorf("unexpected %q in output:\n%s", notWant, got)
		}
	}
}

func TestBuildSnapshotContent_DeduplicatesFilePaths(t *testing.T) {
	lines := []string{
		`{"role":"user","content":"look at cmd/foo.go"}`,
		`{"role":"user","content":"also cmd/bar.go"}`,
		`{"role":"user","content":"back to cmd/foo.go again"}`,
	}
	path := writeJSONLFile(t, lines)

	noop := func(string, ...interface{}) {}
	got := buildSnapshotContent(path, noop)

	count := strings.Count(got, "cmd/foo.go")
	if count != 1 {
		t.Errorf("expected cmd/foo.go to appear once, got %d occurrences in:\n%s", count, got)
	}
}
