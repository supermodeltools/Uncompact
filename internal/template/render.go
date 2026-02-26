package template

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"text/template"
	"unicode"

	"github.com/supermodeltools/uncompact/internal/api"
)

//go:embed *.md.tmpl
var embedded embed.FS

// ContextBombData is the data passed to the context bomb template.
type ContextBombData struct {
	ProjectName         string
	PrimaryLanguage     string
	FileCount           int
	FunctionCount       int
	DomainCount         int
	GeneratedAt         string
	Stale               bool
	Domains             []DomainView
	DomainRelationships []RelationshipView
	Stats               *StatsView
}

// DomainView is a template-friendly domain.
type DomainView struct {
	Name               string
	DescriptionSummary string
	KeyFiles           []string
	Responsibilities   []string
	Subdomains         []SubdomainView
}

// SubdomainView is a template-friendly subdomain.
type SubdomainView struct {
	Name               string
	DescriptionSummary string
}

// RelationshipView is a template-friendly domain relationship.
type RelationshipView struct {
	From string
	To   string
	Type string
}

// StatsView is a template-friendly graph stats summary.
type StatsView struct {
	NodeCount         int64
	RelationshipCount int64
	NodeTypes         map[string]int
}

// RenderContextBomb renders the context bomb for the given IR result.
// maxTokens is used to truncate domains if the output would be too long.
func RenderContextBomb(ir *api.SupermodelIR, projectName string, stale bool, maxTokens int) (string, error) {
	data := buildTemplateData(ir, projectName, stale)

	tmplContent, err := embedded.ReadFile("context_bomb.md.tmpl")
	if err != nil {
		return "", fmt.Errorf("reading template: %w", err)
	}

	tmpl, err := template.New("context_bomb").Funcs(template.FuncMap{
		"join": strings.Join,
	}).Parse(string(tmplContent))
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("rendering template: %w", err)
	}

	output := buf.String()

	// Truncate to approximate token budget if needed
	if maxTokens > 0 {
		output = truncateToTokens(output, maxTokens)
	}

	return output, nil
}

// EstimateTokens provides a rough token count estimate (4 chars ≈ 1 token).
func EstimateTokens(s string) int {
	return len(s) / 4
}

// buildTemplateData converts an API response into template data.
func buildTemplateData(ir *api.SupermodelIR, projectName string, stale bool) ContextBombData {
	data := ContextBombData{
		ProjectName: projectName,
		Stale:       stale,
		GeneratedAt: ir.GeneratedAt,
	}

	// Extract summary fields
	if ir.Summary != nil {
		if v, ok := ir.Summary["primaryLanguage"].(string); ok {
			data.PrimaryLanguage = v
		}
		if v, ok := ir.Summary["filesProcessed"].(float64); ok {
			data.FileCount = int(v)
		}
		if v, ok := ir.Summary["functions"].(float64); ok {
			data.FunctionCount = int(v)
		}
		if v, ok := ir.Summary["domains"].(float64); ok {
			data.DomainCount = int(v)
		}
	}

	// Fallback to metadata
	if data.FileCount == 0 && ir.Metadata != nil {
		data.FileCount = ir.Metadata.FileCount
		if len(ir.Metadata.Languages) > 0 && data.PrimaryLanguage == "" {
			data.PrimaryLanguage = ir.Metadata.Languages[0]
		}
	}
	if data.PrimaryLanguage == "" {
		data.PrimaryLanguage = "unknown"
	}

	// Domains
	data.DomainCount = len(ir.Domains)
	for _, d := range ir.Domains {
		dv := DomainView{
			Name:               d.Name,
			DescriptionSummary: d.DescriptionSummary,
			KeyFiles:           truncateStrings(d.KeyFiles, 4),
			Responsibilities:   truncateStrings(d.Responsibilities, 4),
		}
		for _, sd := range d.Subdomains {
			dv.Subdomains = append(dv.Subdomains, SubdomainView{
				Name:               sd.Name,
				DescriptionSummary: sd.DescriptionSummary,
			})
		}
		data.Domains = append(data.Domains, dv)
	}

	// Domain relationships from graph
	if ir.Graph != nil {
		for _, rel := range ir.Graph.Relationships {
			if rel.Type == "aggregates" || rel.Type == "dependsOn" || rel.Type == "relatesTo" {
				// Only include domain-to-domain relationships
				if strings.HasPrefix(rel.StartNode, "domain:") && strings.HasPrefix(rel.EndNode, "domain:") {
					data.DomainRelationships = append(data.DomainRelationships, RelationshipView{
						From: strings.TrimPrefix(rel.StartNode, "domain:"),
						To:   strings.TrimPrefix(rel.EndNode, "domain:"),
						Type: rel.Type,
					})
				}
			}
		}
	}

	// Stats
	if ir.Stats != nil {
		data.Stats = &StatsView{
			NodeCount:         ir.Stats.NodeCount,
			RelationshipCount: ir.Stats.RelationshipCount,
			NodeTypes:         ir.Stats.NodeTypes,
		}
	}

	return data
}

// truncateStrings returns at most n items from a slice.
func truncateStrings(items []string, n int) []string {
	if len(items) <= n {
		return items
	}
	return items[:n]
}

// truncateToTokens truncates the string to approximately maxTokens tokens,
// cutting at a word boundary.
func truncateToTokens(s string, maxTokens int) string {
	maxChars := maxTokens * 4
	if len(s) <= maxChars {
		return s
	}
	// Cut at last newline before limit
	truncated := s[:maxChars]
	if idx := strings.LastIndexFunc(truncated, func(r rune) bool {
		return r == '\n' || unicode.IsSpace(r)
	}); idx > 0 {
		truncated = truncated[:idx]
	}
	return truncated + "\n\n*[Context truncated — increase --max-tokens to see more]*\n"
}
