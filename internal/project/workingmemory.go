package project

import (
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
	IssueNumber   int
	IssueTitle    string
	IssueBody     string   // truncated to ~500 chars
	BranchCommits []string // git log main..HEAD --oneline, max 10
	ChangedFiles  []string // git diff --stat main..HEAD, file lines only
	Uncommitted   []string // git diff --stat, file lines only
}

var issueNumberRe = regexp.MustCompile(`issue-(\d+)`)

// GetWorkingMemory derives situational context from git and GitHub.
// Returns nil if the branch has no commits ahead of main or on error.
func GetWorkingMemory(rootDir string) *WorkingMemory {
	// Get current branch
	branchOut, err := runGit(rootDir, "branch", "--show-current")
	if err != nil {
		return nil
	}
	branch := strings.TrimSpace(branchOut)

	// Get commits ahead of main — if none, omit working memory entirely
	commitsOut, err := runGit(rootDir, "log", "main..HEAD", "--oneline")
	if err != nil {
		return nil
	}
	commits := parseLines(commitsOut, 10)
	if len(commits) == 0 {
		return nil
	}

	wm := &WorkingMemory{
		Branch:        branch,
		BranchCommits: commits,
	}

	// Parse issue number from branch name
	if m := issueNumberRe.FindStringSubmatch(branch); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			wm.IssueNumber = n
			ghFetchIssue(wm, n)
		}
	}

	// Changed files vs main
	if out, err := runGit(rootDir, "diff", "--stat", "main..HEAD"); err == nil {
		wm.ChangedFiles = parseStatLines(out)
	}

	// Uncommitted changes
	if out, err := runGit(rootDir, "diff", "--stat"); err == nil {
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
// Failures are silently ignored.
func ghFetchIssue(wm *WorkingMemory, issueNumber int) {
	cmd := exec.Command("gh", "issue", "view", fmt.Sprintf("%d", issueNumber), "--json", "title,body")
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
	if len(body) > 500 {
		body = body[:500] + "..."
	}
	wm.IssueBody = body
}
