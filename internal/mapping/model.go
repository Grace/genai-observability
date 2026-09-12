package mapping

import "time"

type Relationship string

const (
	Exact       Relationship = "EXACT"
	Compatible  Relationship = "COMPATIBLE"
	Narrower    Relationship = "NARROWER"
	Broader     Relationship = "BROADER"
	Lossy       Relationship = "LOSSY"
	Conflicting Relationship = "CONFLICTING"
	Unknown     Relationship = "UNKNOWN"
)

type Concept struct {
	System      string         `json:"system"`
	Version     string         `json:"version,omitempty"`
	Field       string         `json:"field"`
	Type        string         `json:"type,omitempty"`
	Unit        string         `json:"unit,omitempty"`
	Description string         `json:"description,omitempty"`
	Examples    []any          `json:"examples,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

type MappingRequest struct {
	ID           string            `json:"id"`
	Source       Concept           `json:"source"`
	Target       Concept           `json:"target"`
	Context      []string          `json:"context,omitempty"`
	CreatedAt    time.Time         `json:"created_at,omitempty"`
	TraceContext map[string]string `json:"trace_context,omitempty"`
}

type Evidence struct {
	Kind   string `json:"kind"`
	Source string `json:"source,omitempty"`
	Claim  string `json:"claim"`
}

type AgentAssessment struct {
	Agent         string       `json:"agent"`
	Relationship  Relationship `json:"relationship"`
	Confidence    float64      `json:"confidence"`
	Rationale     string       `json:"rationale"`
	Evidence      []Evidence   `json:"evidence,omitempty"`
	Objections    []string     `json:"objections,omitempty"`
	Model         string       `json:"model,omitempty"`
	PromptVersion string       `json:"prompt_version,omitempty"`
}

type ReviewInput struct {
	Request     MappingRequest    `json:"request"`
	Assessments []AgentAssessment `json:"assessments"`
}

type MappingAssessment struct {
	ID                  string            `json:"id"`
	Source              Concept           `json:"source"`
	Target              Concept           `json:"target"`
	Relationship        Relationship      `json:"relationship"`
	Confidence          float64           `json:"confidence"`
	Evidence            []Evidence        `json:"evidence,omitempty"`
	Objections          []string          `json:"objections,omitempty"`
	AgentAssessments    []AgentAssessment `json:"agent_assessments"`
	Reviewer            *AgentAssessment  `json:"reviewer,omitempty"`
	RequiresHumanReview bool              `json:"requires_human_review"`
	Decision            string            `json:"decision"`
	PolicyVersion       string            `json:"policy_version"`
	CreatedAt           time.Time         `json:"created_at"`
}
