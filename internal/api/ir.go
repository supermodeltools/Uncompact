package api

import "time"

// SupermodelIR is the raw response from the Supermodel API /v1/graphs/supermodel endpoint.
// To add new API response fields: extend SupermodelIR or the ir* types here, then update
// toProjectGraph below. No changes to client.go are needed for API-to-model mapping.
type SupermodelIR struct {
	Repo     string         `json:"repo"`
	Summary  map[string]any `json:"summary"`
	Metadata irMetadata     `json:"metadata"`
	Domains  []irDomain     `json:"domains"`
	Graph    irGraph        `json:"graph"`
}

type irGraph struct {
	Nodes         []irNode         `json:"nodes"`
	Relationships []irRelationship `json:"relationships"`
}

type irNode struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

type irRelationship struct {
	Type   string `json:"type"`
	Source string `json:"source"`
	Target string `json:"target"`
}

type irMetadata struct {
	FileCount int      `json:"fileCount"`
	Languages []string `json:"languages"`
}

type irDomain struct {
	Name               string        `json:"name"`
	DescriptionSummary string        `json:"descriptionSummary"`
	KeyFiles           []string      `json:"keyFiles"`
	Responsibilities   []string      `json:"responsibilities"`
	Subdomains         []irSubdomain `json:"subdomains"`
}

type irSubdomain struct {
	Name               string `json:"name"`
	DescriptionSummary string `json:"descriptionSummary"`
}

// toProjectGraph converts a SupermodelIR API response into the internal ProjectGraph model.
func (ir *SupermodelIR) toProjectGraph(projectName string) *ProjectGraph {
	lang := ""
	if len(ir.Metadata.Languages) > 0 {
		lang = ir.Metadata.Languages[0]
	}
	if v, ok := ir.Summary["primaryLanguage"]; ok && v != nil {
		if s, ok := v.(string); ok && s != "" {
			lang = s
		}
	}

	// Extract integer fields from the free-form summary map.
	// JSON numbers unmarshal as float64 in map[string]any.
	summaryInt := func(key string) int {
		if v, ok := ir.Summary[key]; ok {
			if n, ok := v.(float64); ok {
				return int(n)
			}
		}
		return 0
	}

	// Build a map of domain → []dependsOn from DOMAIN_RELATES edges.
	// Deduplicates targets and skips self-references.
	dependsOnMap := make(map[string][]string)
	seen := make(map[string]map[string]bool)
	for _, rel := range ir.Graph.Relationships {
		if rel.Type == "DOMAIN_RELATES" && rel.Source != "" && rel.Target != "" && rel.Source != rel.Target {
			if seen[rel.Source] == nil {
				seen[rel.Source] = make(map[string]bool)
			}
			if !seen[rel.Source][rel.Target] {
				seen[rel.Source][rel.Target] = true
				dependsOnMap[rel.Source] = append(dependsOnMap[rel.Source], rel.Target)
			}
		}
	}

	domains := make([]Domain, 0, len(ir.Domains))
	for _, d := range ir.Domains {
		subdomains := make([]Subdomain, 0, len(d.Subdomains))
		for _, s := range d.Subdomains {
			subdomains = append(subdomains, Subdomain{
				Name:        s.Name,
				Description: s.DescriptionSummary,
			})
		}
		domains = append(domains, Domain{
			Name:             d.Name,
			Description:      d.DescriptionSummary,
			KeyFiles:         d.KeyFiles,
			Responsibilities: d.Responsibilities,
			Subdomains:       subdomains,
			DependsOn:        dependsOnMap[d.Name],
		})
	}

	var externalDeps []string
	for _, node := range ir.Graph.Nodes {
		if node.Type == "ExternalDependency" && node.Name != "" {
			externalDeps = append(externalDeps, node.Name)
		}
	}

	return &ProjectGraph{
		Name:         projectName,
		Language:     lang,
		Domains:      domains,
		ExternalDeps: externalDeps,
		Stats: Stats{
			TotalFiles:     summaryInt("filesProcessed"),
			TotalFunctions: summaryInt("functions"),
			Languages:      ir.Metadata.Languages,
		},
		UpdatedAt: time.Now(),
	}
}
