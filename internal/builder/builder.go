// Package builder implements an instrumented agent that proposes a source
// adapter for a telemetry payload from a framework the repository does not yet
// support.
//
// The agent is deliberately a *proposer*, not a code generator: it produces a
// fixture and a per-field mapping proposal, and every field it cannot justify is
// recorded as UNKNOWN rather than mapped. That mirrors the rule the rest of the
// repository follows - an unmapped field is safe, a wrongly equated field is not.
//
// Instrumentation follows OpenTelemetry GenAI semantic conventions 1.41.0: an
// invoke_agent root span, chat spans for model calls, and execute_tool spans for
// each tool invocation, all sharing one gen_ai.conversation.id.
package builder

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/grace/genai-observability/internal/mapping"
	"github.com/grace/genai-observability/internal/pricing"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	// AgentName is the human-readable agent identity carried on every span.
	AgentName = "adapter-builder"
	// PromptVersion identifies the prompt shape so proposals stay attributable
	// after the prompt changes.
	PromptVersion = "adapter-builder-v1"

	tracerName = "genai-observability/builder"
)

// Reply is one model response. Token counts drive cost attribution and are
// reported even when the response fails to parse.
type Reply struct {
	Text         string
	Model        string
	InputTokens  int64
	OutputTokens int64
}

// ModelClient is the agent's dependency on an LLM. Keeping it this small is what
// allows the full loop - including span shape - to be tested without Bedrock.
type ModelClient interface {
	Chat(ctx context.Context, prompt string) (Reply, error)
}

// Options configures one build session.
type Options struct {
	// System is the framework whose payload is being adapted, e.g. "langfuse".
	System string
	// PayloadPath is the sample telemetry payload to learn from.
	PayloadPath string
	// OutDir receives the fixture and proposal. Nothing outside it is written.
	OutDir string
	// MaxIterations bounds the refine loop. Zero means the default.
	MaxIterations int
	// RunTests enables the run_tests tool. Off in unit tests, on in the CLI.
	RunTests bool
	// Catalog prices model calls. The zero value disables cost attribution.
	Catalog pricing.Catalog
}

// FieldProposal is the agent's judgement about a single source field.
type FieldProposal struct {
	SourceField  string               `json:"source_field"`
	TargetField  string               `json:"target_field,omitempty"`
	Relationship mapping.Relationship `json:"relationship"`
	Confidence   float64              `json:"confidence"`
	Rationale    string               `json:"rationale,omitempty"`
}

// Result is the outcome of a build session.
type Result struct {
	System              string          `json:"system"`
	ConversationID      string          `json:"conversation_id"`
	Proposals           []FieldProposal `json:"proposals"`
	UnknownFields       []string        `json:"unknown_fields,omitempty"`
	Iterations          int             `json:"iterations"`
	RequiresHumanReview bool            `json:"requires_human_review"`
	PromptVersion       string          `json:"prompt_version"`
	Model               string          `json:"model,omitempty"`
	CostUSD             *float64        `json:"cost_usd,omitempty"`
	TestsRan            bool            `json:"tests_ran"`
	TestsPassed         bool            `json:"tests_passed"`
	FixturePath         string          `json:"fixture_path,omitempty"`
	ProposalPath        string          `json:"proposal_path,omitempty"`
}

// Run executes one build session and returns its result. The returned error is
// non-nil only when the session could not complete; a session that completes
// with every field UNKNOWN is a successful, conservative outcome.
func Run(ctx context.Context, m ModelClient, opts Options) (Result, error) {
	if opts.MaxIterations <= 0 {
		opts.MaxIterations = 3
	}
	if opts.System == "" {
		return Result{}, fmt.Errorf("builder: System is required")
	}

	tr := otel.Tracer(tracerName)
	conversationID := conversationIDFor(opts)

	ctx, root := tr.Start(ctx, "invoke_agent "+AgentName, trace.WithSpanKind(trace.SpanKindInternal))
	defer root.End()
	root.SetAttributes(
		attribute.String("gen_ai.operation.name", "invoke_agent"),
		attribute.String("gen_ai.agent.name", AgentName),
		attribute.String("gen_ai.agent.id", AgentName+"/"+PromptVersion),
		attribute.String("gen_ai.conversation.id", conversationID),
		attribute.String("builder.system", opts.System),
		attribute.String("builder.prompt_version", PromptVersion),
	)

	res := Result{System: opts.System, ConversationID: conversationID, PromptVersion: PromptVersion}

	payload, fields, err := readPayload(ctx, tr, opts.PayloadPath)
	if err != nil {
		root.RecordError(err)
		root.SetStatus(codes.Error, err.Error())
		return res, err
	}
	root.SetAttributes(attribute.Int("builder.source_field_count", len(fields)))

	var totalIn, totalOut int64
	for i := 1; i <= opts.MaxIterations; i++ {
		res.Iterations = i

		reply, err := chat(ctx, tr, m, prompt(opts.System, fields, res.Proposals), i)
		if err != nil {
			root.RecordError(err)
			root.SetStatus(codes.Error, err.Error())
			return res, err
		}
		totalIn += reply.InputTokens
		totalOut += reply.OutputTokens
		res.Model = reply.Model

		proposals, err := proposeMapping(ctx, tr, reply.Text, fields, i)
		if err != nil {
			// A malformed response is a reason to iterate, not to fail: the next
			// turn sees what survived and is asked again for the rest.
			continue
		}
		res.Proposals = proposals
		if complete(proposals, fields) {
			break
		}
	}

	// Any field the agent never justified stays unmapped and is named explicitly.
	res.Proposals = fillUnknown(res.Proposals, fields)
	res.UnknownFields = unknownFields(res.Proposals)
	res.RequiresHumanReview = requiresReview(res.Proposals)

	if cost, err := pricing.Calculate(opts.Catalog, res.Model, totalIn, totalOut); err == nil {
		res.CostUSD = &cost.USD
		root.SetAttributes(
			attribute.Float64("gen_ai.usage.cost_usd", cost.USD),
			attribute.String("gen_ai.usage.pricing_version", cost.PricingVersion),
		)
	}
	root.SetAttributes(
		attribute.Int64("gen_ai.usage.input_tokens", totalIn),
		attribute.Int64("gen_ai.usage.output_tokens", totalOut),
		attribute.Int("builder.iterations", res.Iterations),
		attribute.Int("builder.unknown_field_count", len(res.UnknownFields)),
		attribute.Bool("builder.requires_human_review", res.RequiresHumanReview),
	)

	if opts.OutDir != "" {
		if err := writeArtifacts(ctx, tr, opts, payload, &res); err != nil {
			root.RecordError(err)
			root.SetStatus(codes.Error, err.Error())
			return res, err
		}
	}
	if opts.RunTests {
		res.TestsRan = true
		res.TestsPassed = runTests(ctx, tr)
		root.SetAttributes(attribute.Bool("builder.tests_passed", res.TestsPassed))
	}
	return res, nil
}

// startTool opens an execute_tool span with the attributes semconv requires for
// tool operations.
func startTool(ctx context.Context, tr trace.Tracer, name string) (context.Context, trace.Span) {
	ctx, span := tr.Start(ctx, "execute_tool "+name)
	span.SetAttributes(
		attribute.String("gen_ai.operation.name", "execute_tool"),
		attribute.String("gen_ai.tool.name", name),
		attribute.String("gen_ai.tool.type", "function"),
		attribute.String("gen_ai.agent.name", AgentName),
	)
	return ctx, span
}

func readPayload(ctx context.Context, tr trace.Tracer, path string) (json.RawMessage, []string, error) {
	_, span := startTool(ctx, tr, "read_payload")
	defer span.End()
	span.SetAttributes(attribute.String("builder.payload_path", path))

	raw, err := os.ReadFile(path)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, nil, fmt.Errorf("read payload: %w", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, nil, fmt.Errorf("parse payload: %w", err)
	}
	fields := flatten(doc, "")
	sort.Strings(fields)
	span.SetAttributes(attribute.Int("builder.field_count", len(fields)))
	return json.RawMessage(raw), fields, nil
}

func chat(ctx context.Context, tr trace.Tracer, m ModelClient, p string, iteration int) (Reply, error) {
	ctx, span := tr.Start(ctx, "chat")
	defer span.End()
	span.SetAttributes(
		attribute.String("gen_ai.operation.name", "chat"),
		attribute.String("gen_ai.agent.name", AgentName),
		attribute.Int("builder.iteration", iteration),
	)
	reply, err := m.Chat(ctx, p)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return reply, err
	}
	span.SetAttributes(
		attribute.String("gen_ai.request.model", reply.Model),
		attribute.String("gen_ai.response.model", reply.Model),
		attribute.Int64("gen_ai.usage.input_tokens", reply.InputTokens),
		attribute.Int64("gen_ai.usage.output_tokens", reply.OutputTokens),
	)
	return reply, nil
}

func proposeMapping(ctx context.Context, tr trace.Tracer, text string, fields []string, iteration int) ([]FieldProposal, error) {
	_, span := startTool(ctx, tr, "propose_mapping")
	defer span.End()
	span.SetAttributes(attribute.Int("builder.iteration", iteration))

	var parsed struct {
		Proposals []FieldProposal `json:"proposals"`
	}
	if err := json.Unmarshal([]byte(extractJSON(text)), &parsed); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "model response was not valid JSON")
		return nil, err
	}

	known := map[string]bool{}
	for _, f := range fields {
		known[f] = true
	}
	out := make([]FieldProposal, 0, len(parsed.Proposals))
	var rejected int
	for _, p := range parsed.Proposals {
		// A proposal for a field that is not in the payload is a hallucination;
		// drop it rather than record it.
		if !known[p.SourceField] {
			rejected++
			continue
		}
		if !validRelationship(p.Relationship) {
			p.Relationship = mapping.Unknown
			p.TargetField = ""
		}
		if p.Relationship == mapping.Unknown {
			p.TargetField = ""
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SourceField < out[j].SourceField })
	span.SetAttributes(
		attribute.Int("builder.proposals_accepted", len(out)),
		attribute.Int("builder.proposals_rejected_unknown_field", rejected),
	)
	return out, nil
}

func writeArtifacts(ctx context.Context, tr trace.Tracer, opts Options, payload json.RawMessage, res *Result) error {
	_, span := startTool(ctx, tr, "write_adapter")
	defer span.End()

	if err := os.MkdirAll(opts.OutDir, 0o755); err != nil {
		span.RecordError(err)
		return err
	}
	fixture := filepath.Join(opts.OutDir, opts.System+".json")
	if err := os.WriteFile(fixture, payload, 0o644); err != nil {
		span.RecordError(err)
		return err
	}
	proposal := filepath.Join(opts.OutDir, opts.System+".mapping.json")
	body, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		span.RecordError(err)
		return err
	}
	if err := os.WriteFile(proposal, append(body, '\n'), 0o644); err != nil {
		span.RecordError(err)
		return err
	}
	res.FixturePath, res.ProposalPath = fixture, proposal
	span.SetAttributes(
		attribute.String("builder.fixture_path", fixture),
		attribute.String("builder.proposal_path", proposal),
		attribute.Int("builder.files_written", 2),
	)
	return nil
}

func runTests(ctx context.Context, tr trace.Tracer) bool {
	_, span := startTool(ctx, tr, "run_tests")
	defer span.End()
	span.SetAttributes(attribute.String("builder.command", "go test ./..."))

	cmd := exec.CommandContext(ctx, "go", "test", "./...")
	out, err := cmd.CombinedOutput()
	passed := err == nil
	span.SetAttributes(attribute.Bool("builder.tests_passed", passed))
	if !passed {
		span.SetStatus(codes.Error, "go test failed")
		span.SetAttributes(attribute.String("builder.test_output_tail", tail(string(out), 400)))
	}
	return passed
}

func prompt(system string, fields []string, prior []FieldProposal) string {
	var b strings.Builder
	b.WriteString("You map telemetry fields from a GenAI framework onto OpenTelemetry GenAI semantic conventions.\n")
	b.WriteString("Framework: " + system + "\n\n")
	b.WriteString("Source fields:\n")
	for _, f := range fields {
		b.WriteString("  - " + f + "\n")
	}
	if len(prior) > 0 {
		b.WriteString("\nAlready decided (do not repeat):\n")
		for _, p := range prior {
			if p.Relationship != mapping.Unknown {
				b.WriteString("  - " + p.SourceField + " -> " + p.TargetField + " (" + string(p.Relationship) + ")\n")
			}
		}
	}
	b.WriteString(`
Return ONLY JSON: {"proposals":[{"source_field","target_field","relationship","confidence","rationale"}]}.
relationship is one of EXACT, COMPATIBLE, NARROWER, BROADER, LOSSY, CONFLICTING, UNKNOWN.
Rules:
- Only propose source_field values from the list above. Never invent a field.
- Never invent a semantic convention attribute you are not confident exists.
- UNKNOWN is the correct answer when meaning, units, or billing rules differ, or when you
  are unsure. An unmapped field is safe; a wrongly equated field corrupts downstream data.
- Prefer LOSSY over EXACT when the target cannot represent everything the source carries.
`)
	return b.String()
}

func flatten(doc map[string]any, prefix string) []string {
	var out []string
	for k, v := range doc {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		if nested, ok := v.(map[string]any); ok && len(nested) > 0 {
			out = append(out, flatten(nested, key)...)
			continue
		}
		out = append(out, key)
	}
	return out
}

// extractJSON tolerates models that wrap JSON in prose or a fenced block.
func extractJSON(s string) string {
	if i := strings.Index(s, "{"); i >= 0 {
		if j := strings.LastIndex(s, "}"); j > i {
			return s[i : j+1]
		}
	}
	return s
}

func validRelationship(r mapping.Relationship) bool {
	switch r {
	case mapping.Exact, mapping.Compatible, mapping.Narrower,
		mapping.Broader, mapping.Lossy, mapping.Conflicting, mapping.Unknown:
		return true
	}
	return false
}

func complete(proposals []FieldProposal, fields []string) bool {
	decided := map[string]bool{}
	for _, p := range proposals {
		if p.Relationship != mapping.Unknown {
			decided[p.SourceField] = true
		}
	}
	for _, f := range fields {
		if !decided[f] {
			return false
		}
	}
	return true
}

// fillUnknown guarantees every source field appears in the result. Silence about
// a field would be indistinguishable from a decision about it.
func fillUnknown(proposals []FieldProposal, fields []string) []FieldProposal {
	seen := map[string]bool{}
	for _, p := range proposals {
		seen[p.SourceField] = true
	}
	for _, f := range fields {
		if !seen[f] {
			proposals = append(proposals, FieldProposal{SourceField: f, Relationship: mapping.Unknown})
		}
	}
	sort.Slice(proposals, func(i, j int) bool { return proposals[i].SourceField < proposals[j].SourceField })
	return proposals
}

func unknownFields(proposals []FieldProposal) []string {
	var out []string
	for _, p := range proposals {
		if p.Relationship == mapping.Unknown {
			out = append(out, p.SourceField)
		}
	}
	return out
}

// requiresReview mirrors internal/mapping's conservatism: anything unknown,
// lossy, conflicting, or low-confidence is a human's decision.
func requiresReview(proposals []FieldProposal) bool {
	for _, p := range proposals {
		switch p.Relationship {
		case mapping.Exact, mapping.Compatible:
			if p.Confidence < 0.90 {
				return true
			}
		default:
			return true
		}
	}
	return false
}

func conversationIDFor(opts Options) string {
	return "build-" + opts.System
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
