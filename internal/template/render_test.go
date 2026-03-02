package template

import (
	"fmt"
	"strings"
	"testing"

	"github.com/supermodeltools/uncompact/internal/api"
	"github.com/supermodeltools/uncompact/internal/project"
	"github.com/supermodeltools/uncompact/internal/snapshot"
)

func TestCountTokens_Empty(t *testing.T) {
	if got := CountTokens(""); got != 0 {
		t.Errorf("CountTokens(\"\") = %d, want 0", got)
	}
}

func TestCountTokens_SingleWord(t *testing.T) {
	got := CountTokens("hello")
	if got < 1 || got > 5 {
		t.Errorf("CountTokens(\"hello\") = %d, want 1–5", got)
	}
}

func TestCountTokens_Sentence(t *testing.T) {
	got := CountTokens("the quick brown fox jumps over the lazy dog")
	// 9 words * 100/75 ≈ 12; 43 chars / 4 = 10; expect max(10,12)=12
	if got < 8 || got > 20 {
		t.Errorf("CountTokens(sentence) = %d, want 8–20", got)
	}
}

func TestCountTokens_CodeSnippet(t *testing.T) {
	code := `func main() {
	fmt.Println("hello, world")
	os.Exit(0)
}`
	got := CountTokens(code)
	if got < 5 || got > 30 {
		t.Errorf("CountTokens(code) = %d, want 5–30", got)
	}
}

func TestCountTokens_NonASCII(t *testing.T) {
	// Japanese text: 7 runes × 3 bytes each = 21 bytes → 21/4 = 5 char estimate.
	// Using byte length instead of rune count avoids severe undercounting for CJK text.
	text := "こんにちは世界"
	got := CountTokens(text)
	minExpected := len([]rune(text)) / 4
	if minExpected < 1 {
		minExpected = 1
	}
	if got < minExpected {
		t.Errorf("CountTokens(non-ASCII) = %d, want >= %d", got, minExpected)
	}
}

func TestCountTokens_LargeString(t *testing.T) {
	large := strings.Repeat("word ", 1000) // 5000 chars, 1000 words
	got := CountTokens(large)
	// charEstimate = 5000/4 = 1250; wordEstimate = 1000*100/75 = 1333; max = 1333
	if got < 1000 || got > 2000 {
		t.Errorf("CountTokens(large) = %d, want 1000–2000", got)
	}
}

func TestCountTokens_Monotonic(t *testing.T) {
	small := CountTokens("hello world")
	large := CountTokens(strings.Repeat("hello world ", 100))
	if large <= small {
		t.Errorf("CountTokens should grow with input: small=%d, large=%d", small, large)
	}
}

func TestCountTokens_CharVsWordDominance(t *testing.T) {
	// Dense identifier like a long hex string has high char count but only 1 word.
	hex := strings.Repeat("a", 400) // 400 chars / 4 = 100; 1 word * 100/75 = 1; charEstimate wins
	got := CountTokens(hex)
	if got < 90 || got > 120 {
		t.Errorf("CountTokens(hex) = %d, want 90–120", got)
	}

	// Lots of short words — word estimate should dominate.
	words := strings.Repeat("hi ", 200) // 200 words * 100/75 = 266; 600 chars / 4 = 150; wordEstimate wins
	got2 := CountTokens(words)
	if got2 < 200 || got2 > 350 {
		t.Errorf("CountTokens(manyWords) = %d, want 200–350", got2)
	}
}

// ── test helpers ─────────────────────────────────────────────────────────────

// testGraph builds a minimal ProjectGraph with the given domains.
func testGraph(domains ...api.Domain) *api.ProjectGraph {
	return &api.ProjectGraph{
		Name:     "TestProject",
		Language: "Go",
		Stats: api.Stats{
			TotalFiles:     10,
			TotalFunctions: 20,
		},
		Domains: domains,
	}
}

// testDomain returns a small domain with the given name and description.
func testDomain(name, desc string) api.Domain {
	return api.Domain{Name: name, Description: desc}
}

// padDomain returns a domain whose description approximates nTokens in token size.
// It uses repeated "word " tokens so that the word estimate dominates CountTokens.
func padDomain(name string, nTokens int) api.Domain {
	wordCount := nTokens * 75 / 100
	if wordCount < 1 {
		wordCount = 1
	}
	return api.Domain{Name: name, Description: strings.Repeat("word ", wordCount)}
}

// ── Render tests ──────────────────────────────────────────────────────────────

func TestRender_FitsWithinBudget(t *testing.T) {
	graph := testGraph(testDomain("core", "Core domain"))
	result, tokens, err := Render(graph, "TestProject", RenderOptions{MaxTokens: 2000})
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	if tokens > 2000 {
		t.Errorf("tokens %d exceeds MaxTokens 2000", tokens)
	}
	if len(result) == 0 {
		t.Error("result must not be empty")
	}
}

func TestRender_TruncatedFitsWithinBudget(t *testing.T) {
	// Flood the graph with large domains to force the truncation path.
	var domains []api.Domain
	for i := 0; i < 20; i++ {
		domains = append(domains, padDomain(fmt.Sprintf("domain%d", i), 200))
	}
	graph := testGraph(domains...)

	const budget = 300
	_, tokens, err := Render(graph, "TestProject", RenderOptions{MaxTokens: budget})
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	if tokens > budget {
		t.Errorf("truncated tokens %d exceeds MaxTokens %d", tokens, budget)
	}
}

func TestRender_PostCompactFitsWithinBudget(t *testing.T) {
	graph := testGraph(testDomain("core", "Core domain"))
	const budget = 2000
	result, tokens, err := Render(graph, "TestProject", RenderOptions{
		MaxTokens:   budget,
		PostCompact: true,
	})
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	if tokens > budget {
		t.Errorf("PostCompact tokens %d exceeds MaxTokens %d", tokens, budget)
	}
	if !strings.Contains(result, "context restored") {
		t.Error("PostCompact result must contain acknowledgment note")
	}
}

func TestRender_ZeroMaxTokensDefaultsTo2000(t *testing.T) {
	graph := testGraph(testDomain("core", "Core domain"))
	_, tokens, err := Render(graph, "TestProject", RenderOptions{MaxTokens: 0})
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	if tokens > 2000 {
		t.Errorf("tokens %d exceeds default 2000 budget", tokens)
	}
}

func TestRender_NegativeMaxTokensDefaultsTo2000(t *testing.T) {
	graph := testGraph(testDomain("core", "Core domain"))
	_, tokens, err := Render(graph, "TestProject", RenderOptions{MaxTokens: -1})
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	if tokens > 2000 {
		t.Errorf("tokens %d exceeds default 2000 budget", tokens)
	}
}

// ── truncateToTokenBudget tests ───────────────────────────────────────────────

func TestTruncate_DomainsDroppedWhenBudgetTight(t *testing.T) {
	// d1 is small enough to fit; d2 is far too large and must be dropped.
	d1 := testDomain("auth", "Handles user login")
	d2 := padDomain("oversized", 300)
	graph := testGraph(d1, d2)

	const budget = 100
	result, tokens, err := truncateToTokenBudget(graph, "TestProject", budget, 0, nil, nil, "", false, false, "")
	if err != nil {
		t.Fatalf("truncateToTokenBudget error: %v", err)
	}
	if tokens > budget {
		t.Errorf("tokens %d exceeds budget %d", tokens, budget)
	}
	if !strings.Contains(result, "auth") {
		t.Error("first domain 'auth' should be present in result")
	}
	if strings.Contains(result, "oversized") {
		t.Error("oversized domain should have been dropped")
	}
}

func TestTruncate_CriticalFilesBeforeDomainMap(t *testing.T) {
	graph := testGraph(testDomain("core", "Core functionality"))
	graph.CriticalFiles = []api.CriticalFile{
		{Path: "main.go", RelationshipCount: 5},
	}

	result, tokens, err := truncateToTokenBudget(graph, "TestProject", 500, 0, nil, nil, "", false, false, "")
	if err != nil {
		t.Fatalf("truncateToTokenBudget error: %v", err)
	}
	if tokens > 500 {
		t.Errorf("tokens %d exceeds budget 500", tokens)
	}

	critIdx := strings.Index(result, "Critical Files")
	domIdx := strings.Index(result, "Domain Map")
	if critIdx < 0 {
		t.Fatal("result does not contain 'Critical Files'")
	}
	if domIdx < 0 {
		t.Fatal("result does not contain 'Domain Map'")
	}
	if critIdx > domIdx {
		t.Errorf("Critical Files (pos %d) must appear before Domain Map (pos %d)", critIdx, domIdx)
	}
}

func TestTruncate_SessionSnapshotIncludedAtHighPriority(t *testing.T) {
	snap := &snapshot.SessionSnapshot{Content: "## Prior session\nContext captured"}
	// Large domain fills most of the budget; snapshot must still appear.
	graph := testGraph(padDomain("bigdomain", 60))

	const budget = 200
	result, tokens, err := truncateToTokenBudget(graph, "TestProject", budget, 0, nil, snap, "", false, false, "")
	if err != nil {
		t.Fatalf("truncateToTokenBudget error: %v", err)
	}
	if tokens > budget {
		t.Errorf("tokens %d exceeds budget %d", tokens, budget)
	}
	if !strings.Contains(result, "Prior session") {
		t.Error("session snapshot content must be present in result")
	}
}

func TestTruncate_WorkingMemoryDroppedGracefully(t *testing.T) {
	wm := &project.WorkingMemory{
		Branch:        "feature/my-feature",
		DefaultBranch: "main",
		BranchCommits: []string{"abc1234 add feature", "def5678 fix tests"},
		ChangedFiles:  []string{"pkg/foo.go | 5 ++-", "pkg/bar.go | 12 ++++"},
		IssueNumber:   42,
		IssueTitle:    "Implement feature Y",
		IssueBody:     "Long issue body with lots of detail about the implementation",
	}
	// Large domain consumes most of the budget, leaving no room for working memory.
	graph := testGraph(padDomain("core", 80))

	const budget = 150
	result, tokens, err := truncateToTokenBudget(graph, "TestProject", budget, 0, wm, nil, "", false, false, "")
	if err != nil {
		t.Fatalf("truncateToTokenBudget error: %v", err)
	}
	if tokens > budget {
		t.Errorf("tokens %d exceeds budget %d even after working memory dropped", tokens, budget)
	}
	if len(result) == 0 {
		t.Error("result must not be empty even when working memory is omitted")
	}
}

func TestTruncate_ExtremelySmallBudgetReturnsFallback(t *testing.T) {
	graph := testGraph(testDomain("core", "Core domain"))

	// Budget of 1 is smaller than any possible required header.
	result, _, err := truncateToTokenBudget(graph, "TestProject", 1, 0, nil, nil, "", false, false, "")
	if err != nil {
		t.Fatalf("truncateToTokenBudget error: %v", err)
	}
	if !strings.Contains(result, "Budget too small") {
		t.Errorf("expected fallback string, got: %q", result)
	}
}

func TestTruncate_ResponsibilitiesAndDependsOnIncluded(t *testing.T) {
	// Domain with Responsibilities and DependsOn — these must survive truncation.
	d := api.Domain{
		Name:             "auth",
		Description:      "Handles authentication",
		Responsibilities: []string{"Verify credentials", "Issue tokens"},
		DependsOn:        []string{"database", "cache"},
	}
	// Large second domain to force the truncation path (it gets dropped).
	big := padDomain("big", 500)
	graph := testGraph(d, big)

	const budget = 300
	result, tokens, err := truncateToTokenBudget(graph, "TestProject", budget, 0, nil, nil, "", false, false, "")
	if err != nil {
		t.Fatalf("truncateToTokenBudget error: %v", err)
	}
	if tokens > budget {
		t.Errorf("tokens %d exceeds budget %d", tokens, budget)
	}
	if !strings.Contains(result, "Verify credentials") {
		t.Errorf("Responsibilities must appear in truncated output; got:\n%s", result)
	}
	if !strings.Contains(result, "Issue tokens") {
		t.Errorf("Responsibilities must appear in truncated output; got:\n%s", result)
	}
	if !strings.Contains(result, "Depends on:") {
		t.Errorf("DependsOn header must appear in truncated output; got:\n%s", result)
	}
	if !strings.Contains(result, "database") {
		t.Errorf("DependsOn items must appear in truncated output; got:\n%s", result)
	}
}

func TestRender_BlockquoteNoTrailingLine(t *testing.T) {
	// Issue bodies ending with \n must not produce a trailing "> " line.
	graph := testGraph(testDomain("core", "Core domain"))
	wm := &project.WorkingMemory{
		Branch:      "feature/test",
		IssueNumber: 1,
		IssueTitle:  "Test issue",
		IssueBody:   "Fix the bug\n",
	}
	result, _, err := Render(graph, "TestProject", RenderOptions{
		MaxTokens:     2000,
		WorkingMemory: wm,
	})
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	if strings.Contains(result, "> \n") {
		t.Errorf("blockquote produced trailing '> ' line; output snippet:\n%s",
			result[strings.Index(result, "Fix the bug"):])
	}
}

func TestRender_BlockquoteMultilineNoTrailingLine(t *testing.T) {
	// Multi-paragraph bodies should render inner blank lines as "> " but not produce a trailing one.
	graph := testGraph(testDomain("core", "Core domain"))
	wm := &project.WorkingMemory{
		Branch:      "feature/test",
		IssueNumber: 1,
		IssueTitle:  "Test issue",
		IssueBody:   "Paragraph one\n\nParagraph two\n",
	}
	result, _, err := Render(graph, "TestProject", RenderOptions{
		MaxTokens:     2000,
		WorkingMemory: wm,
	})
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	if strings.Contains(result, "> \n**") || strings.HasSuffix(strings.TrimRight(result, "\n"), "> ") {
		t.Errorf("blockquote produced trailing '> ' line; output snippet:\n%s",
			result[strings.Index(result, "Paragraph one"):])
	}
}

func TestTruncate_DomainMapHeaderTokensAccounted(t *testing.T) {
	// Regression test for issue #197.
	// The "## Domain Map" header must be counted against the token budget when
	// domain sections are appended; without this accounting the output can
	// silently exceed maxTokens.
	graph := testGraph(
		testDomain("alpha", "Handles authentication and authorisation"),
		testDomain("beta", "Manages the REST API layer and routing"),
	)

	// Test across a range of budgets so the edge case is reliably exercised.
	for budget := 50; budget <= 300; budget += 5 {
		_, tokens, err := truncateToTokenBudget(graph, "Proj", budget, 0, nil, nil, "", false, false, "")
		if err != nil {
			t.Fatalf("budget=%d: error: %v", budget, err)
		}
		if tokens > budget {
			t.Errorf("budget=%d: output tokens %d exceeds budget (domain map header may not be accounted for)", budget, tokens)
		}
	}
}
