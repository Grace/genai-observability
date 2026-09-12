package model

import "time"

type EvalRequest struct {
	TraceID        string            `json:"trace_id"`
	SpanID         string            `json:"span_id,omitempty"`
	Question       string            `json:"question"`
	Answer         string            `json:"answer"`
	Evidence       []string          `json:"evidence,omitempty"`
	Model          string            `json:"model,omitempty"`
	PromptVersion  string            `json:"prompt_version,omitempty"`
	Attributes     map[string]string `json:"attributes,omitempty"`
	TraceContext   map[string]string `json:"trace_context,omitempty"`
	InputTokens    int64             `json:"input_tokens,omitempty"`
	OutputTokens   int64             `json:"output_tokens,omitempty"`
	LatencyMS      float64           `json:"latency_ms,omitempty"`
	CostUSD        *float64          `json:"cost_usd,omitempty"`
	PricingVersion string            `json:"pricing_version,omitempty"`
	ObservedAt     time.Time         `json:"observed_at,omitempty"`
}

type EvalResult struct {
	Name         string            `json:"name"`
	Kind         string            `json:"kind"`
	Value        float64           `json:"value"`
	Pass         *bool             `json:"pass,omitempty"`
	Confidence   *float64          `json:"confidence,omitempty"`
	Reason       string            `json:"reason,omitempty"`
	Evaluator    Evaluator         `json:"evaluator"`
	Semantics    EvalSemantics     `json:"semantics"`
	TraceID      string            `json:"trace_id"`
	TraceContext map[string]string `json:"trace_context,omitempty"`
}

type Evaluator struct {
	Type     string `json:"type"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	Version  string `json:"version,omitempty"`
}

type EvalSemantics struct {
	Concept        string  `json:"concept"`
	ScaleMin       float64 `json:"scale_min"`
	ScaleMax       float64 `json:"scale_max"`
	HigherIsBetter bool    `json:"higher_is_better"`
	Unit           string  `json:"unit,omitempty"`
	Space          string  `json:"space,omitempty"`
}

type Consensus struct {
	MeanScore       float64 `json:"mean_score"`
	StdDev          float64 `json:"stddev"`
	JudgeAgreement  float64 `json:"judge_agreement"`
	MaxDisagreement float64 `json:"max_disagreement"`
	JudgeCount      int     `json:"judge_count"`
}

type HarnessOutput struct {
	Request   EvalRequest  `json:"request"`
	Results   []EvalResult `json:"results"`
	Consensus Consensus    `json:"consensus"`
}

type ReplayCase struct {
	Bucket        string            `json:"bucket"`
	Key           string            `json:"key"`
	Model         string            `json:"model,omitempty"`
	PromptVersion string            `json:"prompt_version,omitempty"`
	Attributes    map[string]string `json:"attributes,omitempty"`
}

type ReplayComparison struct {
	BaselineTraceID   string    `json:"baseline_trace_id"`
	ReplayTraceID     string    `json:"replay_trace_id"`
	BaselineModel     string    `json:"baseline_model,omitempty"`
	ReplayModel       string    `json:"replay_model,omitempty"`
	QualityBaseline   float64   `json:"quality_baseline"`
	QualityReplay     float64   `json:"quality_replay"`
	QualityDelta      float64   `json:"quality_delta"`
	CostBaselineUSD   *float64  `json:"cost_baseline_usd,omitempty"`
	CostReplayUSD     *float64  `json:"cost_replay_usd,omitempty"`
	CostDeltaUSD      *float64  `json:"cost_delta_usd,omitempty"`
	LatencyBaselineMS float64   `json:"latency_baseline_ms"`
	LatencyReplayMS   float64   `json:"latency_replay_ms"`
	LatencyDeltaMS    float64   `json:"latency_delta_ms"`
	PricingVersion    string    `json:"pricing_version,omitempty"`
	ObservedAt        time.Time `json:"observed_at"`
}
