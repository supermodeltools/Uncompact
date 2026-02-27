package project

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// WorkingMemory holds situational context derived from git and GitHub state.
type WorkingMemory struct {
	Branch        string
	DefaultBranch string
	IssueNumber   int
	IssueTitle    string
	IssueBody     string   // truncated to ~500 chars
	BranchCommits []string // git log <default>..HEAD --oneline, max 10
	ChangedFiles  []string // git diff --stat <default>..HEAD, file lines only
	Uncommitted   []string // git diff HEAD --stat, file lines only
}

var issueNumberRe = regexp.MustCompile(`issue-(\d+)`)

// defaultBranch detects the repo's default branch name.
// It first tries git symbolic-ref to read the remote HEAD, then falls back
// to probing "main" and "master" in that order.
func defaultBranch(ctx context.Context, rootDir string) string {
	out, err := runGit(ctx, rootDir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
	if err == nil {
		name := strings.TrimSpace(out)
		// Strip "origin/" prefix
		if idx := strings.Index(name, "/"); idx >= 0 {
			name = name[idx+1:]
		}
		if name != "" {
			return name
		}
	}
	// Fallback: probe for main then master
	for _, candidate := range []string{"main", "master"} {
		if _, err := runGit(ctx, rootDir, "rev-parse", "--verify", candidate); err == nil {
			return candidate
		}
	}
	return "main"
}

// GetWorkingMemory derives situational context from git and GitHub.
// Returns nil if the branch has no commits ahead of the default branch or on error.
func GetWorkingMemory(ctx context.Context, rootDir string) *WorkingMemory {
	// Get current branch
	branchOut, err := runGit(ctx, rootDir, "branch", "--show-current")
	if err != nil {
		return nil
	}
	branch := strings.TrimSpace(branchOut)

	base := defaultBranch(ctx, rootDir)

	// Get commits ahead of the default branch — if none, omit working memory entirely
	commitsOut, err := runGit(ctx, rootDir, "log", base+"..HEAD", "--oneline")
	if err != nil {
		return nil
	}
	commits := parseLines(commitsOut, 10)
	if len(commits) == 0 {
		return nil
	}

	wm := &WorkingMemory{
		Branch:        branch,
		DefaultBranch: base,
		BranchCommits: commits,
	}

	// Parse issue number from branch name
	if m := issueNumberRe.FindStringSubmatch(branch); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			wm.IssueNumber = n
			ghFetchIssue(ctx, wm, n)
		}
	}

	// Changed files vs default branch
	if out, err := runGit(ctx, rootDir, "diff", "--stat", base+"..HEAD"); err == nil {
		wm.ChangedFiles = parseStatLines(out)
	}

	// Uncommitted changes (staged and unstaged)
	if out, err := runGit(ctx, rootDir, "diff", "HEAD", "--stat"); err == nil {
		wm.Uncommitted = parseStatLines(out)
	}

	return wm
}

// parseLines splits git output into non-empty trimmed lines, capped at max.
func parseLines(s string, max int) []string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, line)
		if len(lines) >= max {
			break
		}
	}
	return lines
}

// parseStatLines returns file lines from `git diff --stat` output,
// skipping the summary line (which doesn't contain '|').
func parseStatLines(s string) []string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && strings.Contains(line, "|") {
			lines = append(lines, line)
		}
	}
	return lines
}

// ghFetchIssue populates IssueTitle and IssueBody from the GitHub CLI.
// Failures are silently ignored. The command is cancelled if ctx is done.
func ghFetchIssue(ctx context.Context, wm *WorkingMemory, issueNumber int) {
	cmd := exec.CommandContext(ctx, "gh", "issue", "view", fmt.Sprintf("%d", issueNumber), "--json", "title,body")
	out, err := cmd.Output()
	if err != nil {
		return
	}
	var result struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return
	}
	wm.IssueTitle = result.Title
	body := result.Body
	if len([]rune(body)) > 500 {
		body = string([]rune(body)[:500]) + "..."
	}
	wm.IssueBody = body
}
