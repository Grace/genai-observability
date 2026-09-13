package adapters

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
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
	case "pydanticai", "pydantic-ai":
		return mapAdapter{name: "pydantic-ai", fn: normalizePydanticAI}, nil
	case "eino":
		return mapAdapter{name: "eino", fn: normalizeEino}, nil
	case "genkit", "genkit-go":
		return mapAdapter{name: "genkit", fn: normalizeGenkit}, nil
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
	r.Record.Operation = pick(&r.Warnings, a, "gen_ai.operation.name", "gen_ai.operation.type", "span.name")
	r.Record.Provider = pick(&r.Warnings, a, "gen_ai.provider.name", "gen_ai.system")
	r.Record.Model = pick(&r.Warnings, a, "gen_ai.request.model", "gen_ai.response.model")
	r.Record.InputTokens = pickInt(&r.Warnings, a, "gen_ai.usage.input_tokens", "gen_ai.usage.prompt_tokens")
	r.Record.OutputTokens = pickInt(&r.Warnings, a, "gen_ai.usage.output_tokens", "gen_ai.usage.completion_tokens")
	if total, ok := pickNum(&r.Warnings, a, "gen_ai.usage.total_tokens"); ok {
		// A reported zero is preserved: overwriting it with the synthesized sum
		// erased the disagreement before normalize could flag usage_mismatch.
		r.Record.TotalTokens = total
	} else {
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
	r.Record.Operation = pick(&r.Warnings, a, "gen_ai.operation.name", "llm.request.type", "traceloop.span.kind")
	r.Record.Provider = pick(&r.Warnings, a, "gen_ai.provider.name", "llm.vendor", "gen_ai.system")
	r.Record.Model = pick(&r.Warnings, a, "gen_ai.request.model", "llm.request.model")
	r.Record.InputTokens = pickInt(&r.Warnings, a, "gen_ai.usage.input_tokens", "llm.usage.prompt_tokens")
	r.Record.OutputTokens = pickInt(&r.Warnings, a, "gen_ai.usage.output_tokens", "llm.usage.completion_tokens")
	if total, ok := pickNum(&r.Warnings, a, "gen_ai.usage.total_tokens", "llm.usage.total_tokens"); ok {
		// A reported zero is preserved: overwriting it with the synthesized sum
		// erased the disagreement before normalize could flag usage_mismatch.
		r.Record.TotalTokens = total
	} else {
		r.Record.TotalTokens = r.Record.InputTokens + r.Record.OutputTokens
	}
	copyKnownAttrs(&r.Record, a)
	r.Warnings = append(r.Warnings, issue("source_aliases", "attributes", "OpenLLMetry aliases were canonicalized; inspect extensions before treating unmapped fields as equivalent"))
	return r
}

func normalizeBraintrust(m map[string]any) canonical.NormalizationReport {
	r := base(m)
	r.Record.Operation = pick(&r.Warnings, m, "span_attributes.name", "name", "type")
	r.Record.Model = pick(&r.Warnings, m, "metadata.model", "model")
	r.Record.Provider = pick(&r.Warnings, m, "metadata.provider", "provider")
	r.Record.InputTokens = pickInt(&r.Warnings, m, "metrics.prompt_tokens", "usage.prompt_tokens", "input_tokens")
	r.Record.OutputTokens = pickInt(&r.Warnings, m, "metrics.completion_tokens", "usage.completion_tokens", "output_tokens")
	if total, ok := pickNum(&r.Warnings, m, "metrics.tokens", "usage.total_tokens"); ok {
		// A reported zero is preserved: overwriting it with the synthesized sum
		// erased the disagreement before normalize could flag usage_mismatch.
		r.Record.TotalTokens = total
	} else {
		r.Record.TotalTokens = r.Record.InputTokens + r.Record.OutputTokens
	}
	// Sorted, not map order: evaluations are emitted with index-derived keys
	// downstream, so a nondeterministic order makes the same payload produce
	// different column meanings between runs.
	scores := mapAt(m, "scores")
	names := make([]string, 0, len(scores))
	for k := range scores {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if f, ok := number(scores[k]); ok {
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
	r.Record.Operation = pick(&r.Warnings, m, "operation", "operation_name")
	if r.Record.Operation == "" {
		r.Record.Operation = "chat"
	}
	r.Record.Provider = "aws.bedrock"
	r.Record.Model = pick(&r.Warnings, m, "modelId", "model_id", "model")
	r.Record.InputTokens = pickInt(&r.Warnings, m, "usage.inputTokens", "usage.input_tokens")
	r.Record.OutputTokens = pickInt(&r.Warnings, m, "usage.outputTokens", "usage.output_tokens")
	if total, ok := pickNum(&r.Warnings, m, "usage.totalTokens", "usage.total_tokens"); ok {
		// A reported zero is preserved: overwriting it with the synthesized sum
		// erased the disagreement before normalize could flag usage_mismatch.
		r.Record.TotalTokens = total
	} else {
		r.Record.TotalTokens = r.Record.InputTokens + r.Record.OutputTokens
	}
	if lat, ok := number(path(m, "metrics.latencyMs")); ok {
		r.Record.LatencyMS = &lat
	}
	return r
}

func normalizePydanticAI(m map[string]any) canonical.NormalizationReport {
	// Pydantic AI emits OpenTelemetry-compatible attributes. Keep this adapter
	// explicit so framework-specific attributes can evolve independently.
	r := normalizeOTel(m)
	a := attributes(m)
	if agent := pick(&r.Warnings, a, "pydantic_ai.agent_name", "gen_ai.agent.name"); agent != "" {
		if r.Record.Attributes == nil {
			r.Record.Attributes = map[string]string{}
		}
		r.Record.Attributes["gen_ai.agent.name"] = agent
	}
	return r
}

func normalizeEino(m map[string]any) canonical.NormalizationReport {
	r := base(m)
	r.Record.Operation = mapEinoOperation(pick(&r.Warnings, m, "component", "component_type", "span.component"))
	if op := pick(&r.Warnings, m, "operation", "operation_name"); op != "" {
		r.Record.Operation = op
	}
	r.Record.Provider = pick(&r.Warnings, m, "provider", "vendor")
	r.Record.Model = pick(&r.Warnings, m, "model", "model_name")
	r.Record.InputTokens = pickInt(&r.Warnings, m, "input_tokens", "usage.input_tokens", "usage.prompt_tokens")
	r.Record.OutputTokens = pickInt(&r.Warnings, m, "output_tokens", "usage.output_tokens", "usage.completion_tokens")
	if total, ok := pickNum(&r.Warnings, m, "total_tokens", "usage.total_tokens"); ok {
		// A reported zero is preserved: overwriting it with the synthesized sum
		// erased the disagreement before normalize could flag usage_mismatch.
		r.Record.TotalTokens = total
	} else {
		r.Record.TotalTokens = r.Record.InputTokens + r.Record.OutputTokens
	}
	if lat, ok := number(path(m, "latency_ms")); ok {
		r.Record.LatencyMS = &lat
	}
	return r
}

func mapEinoOperation(component string) string {
	switch strings.ToLower(strings.TrimSpace(component)) {
	case "chatmodel", "chat_model", "model":
		return "chat"
	case "tool", "toolnode":
		return "execute_tool"
	case "retriever", "retrieval":
		return "retrieval"
	default:
		return ""
	}
}

func normalizeGenkit(m map[string]any) canonical.NormalizationReport {
	r := base(m)
	r.Record.Operation = mapGenkitOperation(pick(&r.Warnings, m, "span_type", "type", "operation"))
	provider, modelName := splitProviderModel(pick(&r.Warnings, m, "model", "model_name"))
	r.Record.Provider, r.Record.Model = provider, modelName
	r.Record.InputTokens = pickInt(&r.Warnings, m, "usage.inputTokens", "usage.input_tokens", "usage.promptTokens")
	r.Record.OutputTokens = pickInt(&r.Warnings, m, "usage.outputTokens", "usage.output_tokens", "usage.completionTokens")
	if total, ok := pickNum(&r.Warnings, m, "usage.totalTokens", "usage.total_tokens"); ok {
		// A reported zero is preserved: overwriting it with the synthesized sum
		// erased the disagreement before normalize could flag usage_mismatch.
		r.Record.TotalTokens = total
	} else {
		r.Record.TotalTokens = r.Record.InputTokens + r.Record.OutputTokens
	}
	return r
}

func mapGenkitOperation(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "generate", "model", "chat":
		return "chat"
	case "tool", "tool_run":
		return "execute_tool"
	case "retrieve", "retrieval":
		return "retrieval"
	default:
		return v
	}
}

func splitProviderModel(v string) (string, string) {
	parts := strings.SplitN(v, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", v
}

func base(m map[string]any) canonical.NormalizationReport {
	r := canonical.NormalizationReport{Record: canonical.TelemetryRecord{SchemaVersion: "v1", Extensions: map[string]any{}}}
	r.Record.TraceID = pick(&r.Warnings, m, "trace_id", "traceId", "span.trace_id")
	r.Record.SpanID = pick(&r.Warnings, m, "span_id", "spanId", "span.span_id")
	r.Record.ParentSpanID = pick(&r.Warnings, m, "parent_span_id", "parentSpanId")
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

// pick resolves the first alias that carries a value, and reports when more than
// one alias is present with a different value.
//
// Returning the first match silently was the original behaviour. Two aliases
// disagreeing about the same concept is the ambiguity this project exists to
// surface, and deciding it by argument order hides exactly what should be
// reported. Precedence is still first-listed-wins - that part was never the
// problem - but the conflict is now recorded.
func pick(iss *[]canonical.Issue, m map[string]any, keys ...string) string {
	var chosen, chosenKey string
	var conflicts []string
	for _, k := range keys {
		v := path(m, k)
		if v == nil {
			continue
		}
		s := strings.TrimSpace(fmt.Sprint(v))
		if s == "" || s == "<nil>" {
			continue
		}
		if chosen == "" {
			chosen, chosenKey = s, k
			continue
		}
		if s != chosen {
			conflicts = append(conflicts, k+"="+s)
		}
	}
	if len(conflicts) > 0 && iss != nil {
		*iss = append(*iss, issue("alias_conflict", chosenKey,
			"aliases disagree; kept "+chosenKey+"="+chosen+", also present: "+strings.Join(conflicts, ", ")))
	}
	return chosen
}

// pickNum is pick for numeric fields. It additionally reports a non-integer
// token count instead of truncating it away: a fractional count means something
// upstream is wrong, and int64(10.9) == 10 discards that signal.
func pickNum(iss *[]canonical.Issue, m map[string]any, keys ...string) (int64, bool) {
	var chosen float64
	var chosenKey string
	var found bool
	var conflicts []string
	for _, k := range keys {
		f, ok := number(path(m, k))
		if !ok {
			continue
		}
		if !found {
			chosen, chosenKey, found = f, k, true
			continue
		}
		if f != chosen {
			conflicts = append(conflicts, fmt.Sprintf("%s=%v", k, f))
		}
	}
	if !found {
		return 0, false
	}
	if iss != nil {
		if len(conflicts) > 0 {
			*iss = append(*iss, issue("alias_conflict", chosenKey,
				fmt.Sprintf("aliases disagree; kept %s=%v, also present: %s", chosenKey, chosen, strings.Join(conflicts, ", "))))
		}
		if chosen != math.Trunc(chosen) {
			*iss = append(*iss, issue("non_integer_token_count", chosenKey,
				fmt.Sprintf("%s reported %v, which is not a whole number of tokens; truncated to %d", chosenKey, chosen, int64(chosen))))
		}
	}
	return int64(chosen), true
}

// pickInt is pickNum for fields where absence and zero are equivalent.
func pickInt(iss *[]canonical.Issue, m map[string]any, keys ...string) int64 {
	v, _ := pickNum(iss, m, keys...)
	return v
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
