package canonical

import "time"

// TelemetryRecord is the canonical GenAI telemetry model used between source
// adapters and the OpenTelemetry-aligned exporter. It deliberately keeps the
// normalized surface small and carries source fields that cannot be mapped
// losslessly in Extensions rather than silently discarding them.
type TelemetryRecord struct {
	SchemaVersion string `json:"schema_version"`
	Source        Source `json:"source"`
	TraceID       string `json:"trace_id,omitempty"`
	SpanID        string `json:"span_id,omitempty"`
	ParentSpanID  string `json:"parent_span_id,omitempty"`
	Operation     string `json:"operation"`
	Provider      string `json:"provider,omitempty"`
	Model         string `json:"model,omitempty"`
	// No omitempty on the token counts. A source reporting zero tokens is making
	// a claim, and omitempty erases it into the same wire shape as a source that
	// reported nothing - the exact conflation this model exists to avoid. It
	// defeated the fix upstream too: a preserved total_tokens: 0 raised
	// usage_mismatch internally and then vanished from the response.
	InputTokens   int64             `json:"input_tokens"`
	OutputTokens  int64             `json:"output_tokens"`
	TotalTokens   int64             `json:"total_tokens"`
	CostUSD       *float64          `json:"cost_usd,omitempty"`
	LatencyMS     *float64          `json:"latency_ms,omitempty"`
	Status        string            `json:"status,omitempty"`
	PromptVersion string            `json:"prompt_version,omitempty"`
	Attributes    map[string]string `json:"attributes,omitempty"`
	Evaluations   []Evaluation      `json:"evaluations,omitempty"`
	Extensions    map[string]any    `json:"extensions,omitempty"`
	ObservedAt    time.Time         `json:"observed_at,omitempty"`
}

type Source struct {
	System  string `json:"system"`
	Version string `json:"version,omitempty"`
}

type Evaluation struct {
	Name           string   `json:"name"`
	Concept        string   `json:"concept,omitempty"`
	Kind           string   `json:"kind"`
	Value          float64  `json:"value"`
	Label          string   `json:"label,omitempty"`
	Confidence     *float64 `json:"confidence,omitempty"`
	ScaleMin       *float64 `json:"scale_min,omitempty"`
	ScaleMax       *float64 `json:"scale_max,omitempty"`
	HigherIsBetter *bool    `json:"higher_is_better,omitempty"`
	Evaluator      string   `json:"evaluator,omitempty"`
	Explanation    string   `json:"explanation,omitempty"`
}

// NormalizationReport makes ambiguity and loss visible. A mapping that cannot
// be justified is a warning/error instead of an invented semantic equivalence.
type NormalizationReport struct {
	Record   TelemetryRecord `json:"record"`
	Warnings []Issue         `json:"warnings,omitempty"`
	Errors   []Issue         `json:"errors,omitempty"`
}

type Issue struct {
	Code    string `json:"code"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}
