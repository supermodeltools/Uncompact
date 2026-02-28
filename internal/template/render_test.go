package template

import (
	"strings"
	"testing"
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
	// Japanese text — rune count dominates character estimate
	text := "こんにちは世界"
	got := CountTokens(text)
	if got < 1 {
		t.Errorf("CountTokens(non-ASCII) = %d, want >= 1", got)
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
