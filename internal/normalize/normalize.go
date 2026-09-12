package normalize

import (
	"encoding/json"
	"fmt"

	"github.com/grace/genai-observability/internal/adapters"
	"github.com/grace/genai-observability/internal/canonical"
)

type Request struct {
	Source  string          `json:"source"`
	Version string          `json:"version,omitempty"`
	Payload json.RawMessage `json:"payload"`
}

func Run(req Request) (canonical.NormalizationReport, error) {
	a, err := adapters.For(req.Source)
	if err != nil {
		return canonical.NormalizationReport{}, err
	}
	r, err := a.Normalize(req.Payload)
	if err != nil {
		return canonical.NormalizationReport{}, fmt.Errorf("normalize %s: %w", req.Source, err)
	}
	r.Record.Source.Version = req.Version
	validate(&r)
	return r, nil
}

func validate(r *canonical.NormalizationReport) {
	if r.Record.Operation == "" {
		r.Errors = append(r.Errors, canonical.Issue{Code: "required_field", Field: "operation", Message: "canonical operation is required"})
	}
	if r.Record.InputTokens < 0 || r.Record.OutputTokens < 0 || r.Record.TotalTokens < 0 {
		r.Errors = append(r.Errors, canonical.Issue{Code: "invalid_usage", Field: "usage", Message: "token counts cannot be negative"})
	}
	if r.Record.TotalTokens > 0 && r.Record.InputTokens+r.Record.OutputTokens > 0 && r.Record.TotalTokens != r.Record.InputTokens+r.Record.OutputTokens {
		r.Warnings = append(r.Warnings, canonical.Issue{Code: "usage_mismatch", Field: "total_tokens", Message: "reported total tokens differ from input + output; preserved without rewriting"})
	}
}
