package api

// JobStatus represents the async job envelope status.
type JobStatus struct {
	Status     string `json:"status"` // pending, processing, completed, failed
	JobID      string `json:"jobId"`
	RetryAfter int    `json:"retryAfter,omitempty"`
	Error      string `json:"error,omitempty"`
}

// SupermodelIRAsync is the async envelope for the /v1/graphs/supermodel endpoint.
type SupermodelIRAsync struct {
	JobStatus
	Result *SupermodelIR `json:"result,omitempty"`
}

// SupermodelIR is the full Supermodel Intermediate Representation.
type SupermodelIR struct {
	Repo          string                 `json:"repo"`
	Version       string                 `json:"version"`
	SchemaVersion string                 `json:"schemaVersion"`
	GeneratedAt   string                 `json:"generatedAt"`
	Summary       map[string]interface{} `json:"summary,omitempty"`
	Stats         *GraphStats            `json:"stats,omitempty"`
	Metadata      *AnalysisMetadata      `json:"metadata,omitempty"`
	Domains       []DomainSummary        `json:"domains"`
	Graph         *CodeGraph             `json:"graph"`
	Artifacts     []Artifact             `json:"artifacts,omitempty"`
}

// CodeGraph holds nodes and relationships.
type CodeGraph struct {
	Nodes         []CodeGraphNode         `json:"nodes"`
	Relationships []CodeGraphRelationship `json:"relationships"`
}

// CodeGraphNode is a node in the code graph.
type CodeGraphNode struct {
	ID         string                 `json:"id"`
	Labels     []string               `json:"labels"`
	Properties map[string]interface{} `json:"properties,omitempty"`
}

// CodeGraphRelationship is a relationship in the code graph.
type CodeGraphRelationship struct {
	ID         string                 `json:"id"`
	Type       string                 `json:"type"`
	StartNode  string                 `json:"startNode"`
	EndNode    string                 `json:"endNode"`
	Properties map[string]interface{} `json:"properties,omitempty"`
}

// GraphStats holds aggregate graph statistics.
type GraphStats struct {
	NodeCount         int64          `json:"nodeCount"`
	RelationshipCount int64          `json:"relationshipCount"`
	NodeTypes         map[string]int `json:"nodeTypes,omitempty"`
	RelationshipTypes map[string]int `json:"relationshipTypes,omitempty"`
}

// AnalysisMetadata holds timing and file info.
type AnalysisMetadata struct {
	AnalysisStartTime string   `json:"analysisStartTime"`
	AnalysisEndTime   string   `json:"analysisEndTime"`
	FileCount         int      `json:"fileCount"`
	Languages         []string `json:"languages,omitempty"`
}

// DomainSummary describes a discovered domain.
type DomainSummary struct {
	Name               string           `json:"name"`
	DescriptionSummary string           `json:"descriptionSummary"`
	KeyFiles           []string         `json:"keyFiles"`
	Responsibilities   []string         `json:"responsibilities"`
	Subdomains         []SubdomainSummary `json:"subdomains"`
}

// SubdomainSummary describes a subdomain within a domain.
type SubdomainSummary struct {
	Name               string   `json:"name"`
	DescriptionSummary string   `json:"descriptionSummary"`
	Files              []string `json:"files"`
	Functions          []string `json:"functions"`
	Classes            []string `json:"classes"`
}

// Artifact is a per-source analysis artifact.
type Artifact struct {
	ID       string                 `json:"id"`
	Kind     string                 `json:"kind"`
	Label    string                 `json:"label"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// APIError is the standard API error response.
type APIError struct {
	Status    int    `json:"status"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
	RequestID string `json:"requestId,omitempty"`
}

func (e *APIError) Error() string {
	return e.Message
}
