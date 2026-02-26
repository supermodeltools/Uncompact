package output

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/supermodeltools/uncompact/internal/api"
)

const contextBombTemplate = `# Uncompact Context

> Re-injected by Uncompact · {{.Timestamp}} · ~{{.EstimatedTokens}} tokens{{if .IsStale}} · ⚠ STALE: last updated {{.StaleAge}} ago{{end}}

{{range .Sections}}{{if eq .Priority "required"}}
## {{.Title}}

{{.Content}}
{{end}}{{end}}
{{range .Sections}}{{if eq .Priority "optional"}}
## {{.Title}}

{{.Content}}
{{end}}{{end}}`

// ContextBombData is the template data for rendering the context bomb.
type ContextBombData struct {
	Timestamp       string
	EstimatedTokens int
	IsStale         bool
	StaleAge        string
	Sections        []api.GraphSection
}

// RenderContextBomb renders the graph into a Markdown context bomb capped at maxTokens.
func RenderContextBomb(graph *api.Graph, maxTokens int) (string, error) {
	if graph == nil {
		return FallbackContext(), nil
	}

	isStale := time.Since(graph.FetchedAt) > graph.TTL
	staleAge := ""
	if isStale {
		staleAge = formatAge(time.Since(graph.FetchedAt))
	}

	// Build sections list, respecting token budget
	sections, totalTokens := selectSections(graph.Sections, maxTokens)

	data := ContextBombData{
		Timestamp:       graph.FetchedAt.Format("2006-01-02 15:04 UTC"),
		EstimatedTokens: totalTokens,
		IsStale:         isStale,
		StaleAge:        staleAge,
		Sections:        sections,
	}

	tmpl, err := template.New("context-bomb").Parse(contextBombTemplate)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}

	return buf.String(), nil
}

// FallbackContext returns minimal static context when the API and cache are both unavailable.
func FallbackContext() string {
	return strings.TrimSpace(`
# Uncompact Context

> ⚠ Uncompact could not reach the Supermodel API and no cached context is available.
> Run \`uncompact status\` for details or \`uncompact run --force-refresh\` to retry.
`) + "\n"
}

// selectSections picks sections to include within the token budget.
// Required sections are always included first; optional sections fill the remainder.
func selectSections(sections []api.GraphSection, maxTokens int) ([]api.GraphSection, int) {
	var required, optional []api.GraphSection
	for _, s := range sections {
		if s.Priority == "required" {
			required = append(required, s)
		} else {
			optional = append(optional, s)
		}
	}

	var selected []api.GraphSection
	remaining := maxTokens

	for _, s := range required {
		if s.Tokens > 0 && remaining-s.Tokens < 0 {
			continue // skip even required sections if truly no budget left
		}
		selected = append(selected, s)
		remaining -= s.Tokens
	}

	for _, s := range optional {
		if remaining <= 0 {
			break
		}
		if s.Tokens > 0 && s.Tokens > remaining {
			continue
		}
		selected = append(selected, s)
		remaining -= s.Tokens
	}

	totalTokens := maxTokens - remaining
	return selected, totalTokens
}

func formatAge(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh", int(d.Hours()))
}
