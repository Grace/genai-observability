package adapters

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/grace/genai-observability/internal/canonical"
)

type Adapter interface {
	Name() string
	Normalize(raw json.RawMessage) (canonical.NormalizationReport, error)
}

func For(source string) (Adapter, error) {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "otel", "opentelemetry", "otel-genai":
		return mapAdapter{name: "opentelemetry", fn: normalizeOTel}, nil
	case "openllmetry", "traceloop":
		return mapAdapter{name: "openllmetry", fn: normalizeOpenLLMetry}, nil
	case "braintrust":
		return mapAdapter{name: "braintrust", fn: normalizeBraintrust}, nil
	case "bedrock", "aws-bedrock":
		return mapAdapter{name: "aws-bedrock", fn: normalizeBedrock}, nil
	default:
		return nil, fmt.Errorf("unsupported telemetry source %q", source)
	}
}

type mapAdapter struct {
	name string
	fn   func(map[string]any) canonical.NormalizationReport
}

func (a mapAdapter) Name() string { return a.name }
func (a mapAdapter) Normalize(raw json.RawMessage) (canonical.NormalizationReport, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return canonical.NormalizationReport{}, err
	}
	r := a.fn(m)
	r.Record.Source.System = a.name
	if r.Record.SchemaVersion == "" {
		r.Record.SchemaVersion = "v1"
	}
	if r.Record.ObservedAt.IsZero() {
		r.Record.ObservedAt = time.Now().UTC()
	}
	return r, nil
}

func normalizeOTel(m map[string]any) canonical.NormalizationReport {
	r := base(m)
	a := attributes(m)
	r.Record.Operation = firstString(a, "gen_ai.operation.name", "gen_ai.operation.type", "span.name")
	r.Record.Provider = firstString(a, "gen_ai.provider.name", "gen_ai.system")
	r.Record.Model = firstString(a, "gen_ai.request.model", "gen_ai.response.model")
	r.Record.InputTokens = firstInt(a, "gen_ai.usage.input_tokens", "gen_ai.usage.prompt_tokens")
	r.Record.OutputTokens = firstInt(a, "gen_ai.usage.output_tokens", "gen_ai.usage.completion_tokens")
	r.Record.TotalTokens = firstInt(a, "gen_ai.usage.total_tokens")
	if r.Record.TotalTokens == 0 {
		r.Record.TotalTokens = r.Record.InputTokens + r.Record.OutputTokens
	}
	copyKnownAttrs(&r.Record, a)
	if r.Record.Operation == "" {
		r.Warnings = append(r.Warnings, issue("missing_operation", "gen_ai.operation.name", "no GenAI operation name was present"))
	}
	return r
}

func normalizeOpenLLMetry(m map[string]any) canonical.NormalizationReport {
	r := base(m)
	a := attributes(m)
	// OpenLLMetry has historically emitted a mixture of OpenTelemetry GenAI and
	// vendor-prefixed attributes. Accept known aliases but record the alias used.
	r.Record.Operation = firstString(a, "gen_ai.operation.name", "llm.request.type", "traceloop.span.kind")
	r.Record.Provider = firstString(a, "gen_ai.provider.name", "llm.vendor", "gen_ai.system")
	r.Record.Model = firstString(a, "gen_ai.request.model", "llm.request.model")
	r.Record.InputTokens = firstInt(a, "gen_ai.usage.input_tokens", "llm.usage.prompt_tokens")
	r.Record.OutputTokens = firstInt(a, "gen_ai.usage.output_tokens", "llm.usage.completion_tokens")
	r.Record.TotalTokens = firstInt(a, "gen_ai.usage.total_tokens", "llm.usage.total_tokens")
	if r.Record.TotalTokens == 0 {
		r.Record.TotalTokens = r.Record.InputTokens + r.Record.OutputTokens
	}
	copyKnownAttrs(&r.Record, a)
	r.Warnings = append(r.Warnings, issue("source_aliases", "attributes", "OpenLLMetry aliases were canonicalized; inspect extensions before treating unmapped fields as equivalent"))
	return r
}

func normalizeBraintrust(m map[string]any) canonical.NormalizationReport {
	r := base(m)
	r.Record.Operation = firstString(m, "span_attributes.name", "name", "type")
	r.Record.Model = firstString(m, "metadata.model", "model")
	r.Record.Provider = firstString(m, "metadata.provider", "provider")
	r.Record.InputTokens = firstInt(m, "metrics.prompt_tokens", "usage.prompt_tokens", "input_tokens")
	r.Record.OutputTokens = firstInt(m, "metrics.completion_tokens", "usage.completion_tokens", "output_tokens")
	r.Record.TotalTokens = firstInt(m, "metrics.tokens", "usage.total_tokens")
	if r.Record.TotalTokens == 0 {
		r.Record.TotalTokens = r.Record.InputTokens + r.Record.OutputTokens
	}
	for k, v := range mapAt(m, "scores") {
		if f, ok := number(v); ok {
			r.Record.Evaluations = append(r.Record.Evaluations, canonical.Evaluation{Name: k, Concept: k, Kind: "scalar", Value: f, Evaluator: "braintrust"})
		}
	}
	if len(r.Record.Evaluations) > 0 {
		r.Warnings = append(r.Warnings, issue("unknown_score_semantics", "scores", "Braintrust scores are preserved as scalar values unless scale/direction semantics are explicitly supplied"))
	}
	return r
}

func normalizeBedrock(m map[string]any) canonical.NormalizationReport {
	r := base(m)
	r.Record.Operation = firstString(m, "operation", "operation_name")
	if r.Record.Operation == "" {
		r.Record.Operation = "chat"
	}
	r.Record.Provider = "aws.bedrock"
	r.Record.Model = firstString(m, "modelId", "model_id", "model")
	r.Record.InputTokens = firstInt(m, "usage.inputTokens", "usage.input_tokens")
	r.Record.OutputTokens = firstInt(m, "usage.outputTokens", "usage.output_tokens")
	r.Record.TotalTokens = firstInt(m, "usage.totalTokens", "usage.total_tokens")
	if r.Record.TotalTokens == 0 {
		r.Record.TotalTokens = r.Record.InputTokens + r.Record.OutputTokens
	}
	if lat, ok := number(path(m, "metrics.latencyMs")); ok {
		r.Record.LatencyMS = &lat
	}
	return r
}

func base(m map[string]any) canonical.NormalizationReport {
	r := canonical.NormalizationReport{Record: canonical.TelemetryRecord{SchemaVersion: "v1", Extensions: map[string]any{}}}
	r.Record.TraceID = firstString(m, "trace_id", "traceId", "span.trace_id")
	r.Record.SpanID = firstString(m, "span_id", "spanId", "span.span_id")
	r.Record.ParentSpanID = firstString(m, "parent_span_id", "parentSpanId")
	for k, v := range m {
		r.Record.Extensions[k] = v
	}
	return r
}

func copyKnownAttrs(r *canonical.TelemetryRecord, a map[string]any) {
	r.Attributes = map[string]string{}
	for k, v := range a {
		if strings.HasPrefix(k, "gen_ai.") || strings.HasPrefix(k, "server.") || strings.HasPrefix(k, "error.") {
			r.Attributes[k] = fmt.Sprint(v)
		}
	}
}

func attributes(m map[string]any) map[string]any {
	if a, ok := m["attributes"].(map[string]any); ok {
		return a
	}
	return m
}
func mapAt(m map[string]any, key string) map[string]any {
	v := path(m, key)
	if x, ok := v.(map[string]any); ok {
		return x
	}
	return map[string]any{}
}
func path(m map[string]any, dotted string) any {
	if v, ok := m[dotted]; ok {
		return v
	}
	parts := strings.Split(dotted, ".")
	var cur any = m
	for _, p := range parts {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[p]
	}
	return cur
}
func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v := path(m, k); v != nil {
			if s := strings.TrimSpace(fmt.Sprint(v)); s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}
func firstInt(m map[string]any, keys ...string) int64 {
	for _, k := range keys {
		if f, ok := number(path(m, k)); ok {
			return int64(f)
		}
	}
	return 0
}
func number(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		f, e := x.Float64()
		return f, e == nil
	case string:
		f, e := strconv.ParseFloat(x, 64)
		return f, e == nil
	default:
		return 0, false
	}
}
func issue(code, field, msg string) canonical.Issue {
	return canonical.Issue{Code: code, Field: field, Message: msg}
}
