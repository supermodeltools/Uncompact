package project

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- parseLines ---

func TestParseLines_Empty(t *testing.T) {
	result := parseLines("", 10)
	if len(result) != 0 {
		t.Errorf("parseLines(\"\", 10) = %v, want empty slice", result)
	}
}

func TestParseLines_SingleLine(t *testing.T) {
	result := parseLines("hello\n", 10)
	if len(result) != 1 || result[0] != "hello" {
		t.Errorf("parseLines = %v, want [\"hello\"]", result)
	}
}

func TestParseLines_TrimsWhitespace(t *testing.T) {
	result := parseLines("  hello  \n  world  \n", 10)
	if len(result) != 2 {
		t.Fatalf("parseLines len = %d, want 2", len(result))
	}
	if result[0] != "hello" || result[1] != "world" {
		t.Errorf("parseLines = %v, want [\"hello\", \"world\"]", result)
	}
}

func TestParseLines_SkipsEmptyLines(t *testing.T) {
	result := parseLines("a\n\nb\n\n", 10)
	if len(result) != 2 {
		t.Errorf("parseLines len = %d, want 2 (empty lines skipped); result=%v", len(result), result)
	}
}

func TestParseLines_CapAtMax(t *testing.T) {
	input := "line1\nline2\nline3\nline4\nline5\n"
	result := parseLines(input, 3)
	if len(result) != 3 {
		t.Errorf("parseLines len = %d, want 3 (capped at max)", len(result))
	}
}

// --- parseStatLines ---

func TestParseStatLines_Empty(t *testing.T) {
	result := parseStatLines("", 20)
	if len(result) != 0 {
		t.Errorf("parseStatLines(\"\", 20) = %v, want empty", result)
	}
}

func TestParseStatLines_IncludesFileLines(t *testing.T) {
	input := " internal/api/client.go | 50 ++++\n 2 files changed, 50 insertions(+)\n"
	result := parseStatLines(input, 20)
	if len(result) != 1 {
		t.Fatalf("parseStatLines len = %d, want 1; result=%v", len(result), result)
	}
	if !strings.Contains(result[0], "internal/api/client.go") {
		t.Errorf("parseStatLines[0] = %q, want it to contain the file name", result[0])
	}
}

func TestParseStatLines_SkipsSummaryLine(t *testing.T) {
	// The summary line does not contain '|' and must be filtered out.
	input := " foo.go | 10 ++\n bar.go | 5 +\n 2 files changed, 15 insertions(+)\n"
	result := parseStatLines(input, 20)
	if len(result) != 2 {
		t.Errorf("parseStatLines len = %d, want 2 (summary skipped); result=%v", len(result), result)
	}
}

func TestParseStatLines_CapAtMax(t *testing.T) {
	var lines []string
	for i := 0; i < 25; i++ {
		lines = append(lines, fmt.Sprintf(" file%d.go | %d +", i, i+1))
	}
	input := strings.Join(lines, "\n") + "\n"
	result := parseStatLines(input, 10)
	if len(result) != 10 {
		t.Errorf("parseStatLines len = %d, want 10 (capped at max)", len(result))
	}
}

// --- ghFetchIssue ---

// createFakeGh writes a shell script named "gh" into a temp dir that outputs the
// given string and exits with exitCode. It prepends that dir to PATH and returns
// a cleanup function that restores the original PATH.
func createFakeGh(t *testing.T, output string, exitCode int) func() {
	t.Helper()
	dir := t.TempDir()
	dataFile := filepath.Join(dir, "gh_output")
	if err := os.WriteFile(dataFile, []byte(output), 0644); err != nil {
		t.Fatalf("createFakeGh: write output: %v", err)
	}
	script := fmt.Sprintf("#!/bin/sh\ncat %s\nexit %d\n", dataFile, exitCode)
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0755); err != nil {
		t.Fatalf("createFakeGh: write script: %v", err)
	}
	orig := os.Getenv("PATH")
	os.Setenv("PATH", dir+":"+orig)
	return func() { os.Setenv("PATH", orig) }
}

func TestGhFetchIssue_GhUnavailable(t *testing.T) {
	// Point PATH at an empty dir so exec.CommandContext("gh", ...) fails to find gh.
	dir := t.TempDir()
	orig := os.Getenv("PATH")
	os.Setenv("PATH", dir)
	defer os.Setenv("PATH", orig)

	wm := &WorkingMemory{}
	var warned bool
	logFn := func(msg string, args ...interface{}) { warned = true }
	ghFetchIssue(context.Background(), wm, 42, logFn)

	// ghFetchIssue must silently ignore the failure.
	if wm.IssueTitle != "" || wm.IssueBody != "" {
		t.Errorf("expected empty IssueTitle/IssueBody when gh is unavailable; got title=%q body=%q",
			wm.IssueTitle, wm.IssueBody)
	}
	if warned {
		t.Error("logFn should not be called when gh is unavailable")
	}
}

func TestGhFetchIssue_MalformedJSON(t *testing.T) {
	cleanup := createFakeGh(t, "not valid json", 0)
	defer cleanup()

	wm := &WorkingMemory{}
	logFn := func(msg string, args ...interface{}) {}
	ghFetchIssue(context.Background(), wm, 1, logFn)

	if wm.IssueTitle != "" || wm.IssueBody != "" {
		t.Errorf("expected empty fields for malformed JSON; got title=%q body=%q",
			wm.IssueTitle, wm.IssueBody)
	}
}

func TestGhFetchIssue_TruncatesLongBody(t *testing.T) {
	longBody := strings.Repeat("x", 3000) // well over the 2000-rune limit
	payload := fmt.Sprintf(`{"title":"issue-title","body":%q}`, longBody)
	cleanup := createFakeGh(t, payload, 0)
	defer cleanup()

	var warnMsg string
	logFn := func(msg string, args ...interface{}) {
		warnMsg = fmt.Sprintf(msg, args...)
	}
	wm := &WorkingMemory{}
	ghFetchIssue(context.Background(), wm, 1, logFn)

	if wm.IssueTitle != "issue-title" {
		t.Errorf("IssueTitle = %q, want %q", wm.IssueTitle, "issue-title")
	}
	bodyRunes := []rune(wm.IssueBody)
	if len(bodyRunes) > 2010 { // 2000 runes + "..." suffix = 2003
		t.Errorf("IssueBody rune count = %d, want ≤2010 after truncation", len(bodyRunes))
	}
	if !strings.HasSuffix(wm.IssueBody, "...") {
		t.Errorf("truncated IssueBody should end with \"...\"; body rune count = %d", len(bodyRunes))
	}
	if warnMsg == "" {
		t.Error("logFn should have been called to warn about truncation")
	}
}

func TestGhFetchIssue_ShortBodyNoTruncation(t *testing.T) {
	shortBody := "A short issue body."
	payload := fmt.Sprintf(`{"title":"short-issue","body":%q}`, shortBody)
	cleanup := createFakeGh(t, payload, 0)
	defer cleanup()

	var warned bool
	logFn := func(msg string, args ...interface{}) { warned = true }
	wm := &WorkingMemory{}
	ghFetchIssue(context.Background(), wm, 1, logFn)

	if wm.IssueBody != shortBody {
		t.Errorf("IssueBody = %q, want %q", wm.IssueBody, shortBody)
	}
	if warned {
		t.Error("logFn should not be called for short body")
	}
}
