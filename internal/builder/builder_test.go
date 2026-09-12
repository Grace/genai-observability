package builder

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/grace/genai-observability/internal/mapping"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

type fakeModel struct {
	replies []Reply
	calls   int
	prompts []string
}

func (f *fakeModel) Chat(_ context.Context, p string) (Reply, error) {
	f.prompts = append(f.prompts, p)
	i := f.calls
	f.calls++
	if i >= len(f.replies) {
		i = len(f.replies) - 1
	}
	return f.replies[i], nil
}

// recordSpans installs an in-memory exporter and returns the spans produced by fn.
func recordSpans(t *testing.T, fn func(context.Context)) tracetest.SpanStubs {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	fn(context.Background())
	if err := tp.ForceFlush(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}
	return exp.GetSpans()
}

func writePayload(t *testing.T, dir string, doc map[string]any) string {
	t.Helper()
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "payload.json")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func attrString(s tracetest.SpanStub, key string) string {
	for _, kv := range s.Attributes {
		if string(kv.Key) == key {
			return kv.Value.AsString()
		}
	}
	return ""
}

func findSpan(spans tracetest.SpanStubs, name string) (tracetest.SpanStub, bool) {
	for _, s := range spans {
		if s.Name == name {
			return s, true
		}
	}
	return tracetest.SpanStub{}, false
}

func okReply() Reply {
	return Reply{
		Model:        "anthropic.claude-test",
		InputTokens:  120,
		OutputTokens: 45,
		Text: `{"proposals":[
			{"source_field":"usage.inputTokens","target_field":"gen_ai.usage.input_tokens","relationship":"EXACT","confidence":0.97},
			{"source_field":"model","target_field":"gen_ai.request.model","relationship":"EXACT","confidence":0.95}
		]}`,
	}
}

func basePayload() map[string]any {
	return map[string]any{
		"model": "gpt-4.1",
		"usage": map[string]any{"inputTokens": 812},
	}
}

func TestRunEmitsAgentAndToolSpans(t *testing.T) {
	dir := t.TempDir()
	payload := writePayload(t, dir, basePayload())
	m := &fakeModel{replies: []Reply{okReply()}}

	var res Result
	spans := recordSpans(t, func(ctx context.Context) {
		var err error
		res, err = Run(ctx, m, Options{System: "acme", PayloadPath: payload, OutDir: dir})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	})

	root, ok := findSpan(spans, "invoke_agent "+AgentName)
	if !ok {
		t.Fatalf("missing invoke_agent root span; got %v", spanNames(spans))
	}
	if got := attrString(root, "gen_ai.operation.name"); got != "invoke_agent" {
		t.Errorf("root gen_ai.operation.name = %q, want invoke_agent", got)
	}
	if got := attrString(root, "gen_ai.agent.name"); got != AgentName {
		t.Errorf("root gen_ai.agent.name = %q, want %q", got, AgentName)
	}
	if attrString(root, "gen_ai.conversation.id") == "" {
		t.Error("root span is missing gen_ai.conversation.id")
	}

	// Every tool the agent ran must appear as its own execute_tool span.
	for _, tool := range []string{"read_payload", "propose_mapping", "write_adapter"} {
		s, ok := findSpan(spans, "execute_tool "+tool)
		if !ok {
			t.Fatalf("missing execute_tool span for %q; got %v", tool, spanNames(spans))
		}
		if got := attrString(s, "gen_ai.operation.name"); got != "execute_tool" {
			t.Errorf("%s gen_ai.operation.name = %q, want execute_tool", tool, got)
		}
		if got := attrString(s, "gen_ai.tool.name"); got != tool {
			t.Errorf("gen_ai.tool.name = %q, want %q", got, tool)
		}
		if got := attrString(s, "gen_ai.tool.type"); got != "function" {
			t.Errorf("%s gen_ai.tool.type = %q, want function", tool, got)
		}
	}

	chatSpan, ok := findSpan(spans, "chat")
	if !ok {
		t.Fatal("missing chat span")
	}
	if got := attrString(chatSpan, "gen_ai.operation.name"); got != "chat" {
		t.Errorf("chat gen_ai.operation.name = %q, want chat", got)
	}
	if got := attrString(chatSpan, "gen_ai.request.model"); got != "anthropic.claude-test" {
		t.Errorf("chat gen_ai.request.model = %q", got)
	}
	if res.ConversationID == "" {
		t.Error("result is missing conversation id")
	}
}

func TestAllSpansShareOneTrace(t *testing.T) {
	dir := t.TempDir()
	payload := writePayload(t, dir, basePayload())
	m := &fakeModel{replies: []Reply{okReply()}}

	spans := recordSpans(t, func(ctx context.Context) {
		if _, err := Run(ctx, m, Options{System: "acme", PayloadPath: payload, OutDir: dir}); err != nil {
			t.Fatalf("Run: %v", err)
		}
	})

	var traceID trace.TraceID
	for _, s := range spans {
		if !traceID.IsValid() {
			traceID = s.SpanContext.TraceID()
			continue
		}
		if s.SpanContext.TraceID() != traceID {
			t.Fatalf("span %q is on a different trace; the agent must emit one connected trace", s.Name)
		}
	}
	// Tool spans must descend from the agent root, or the timeline shows a flat list.
	root, ok := findSpan(spans, "invoke_agent "+AgentName)
	if !ok {
		t.Fatal("missing invoke_agent root span")
	}
	tool, ok := findSpan(spans, "execute_tool read_payload")
	if !ok {
		t.Fatal("missing execute_tool read_payload span")
	}
	if !root.SpanContext.SpanID().IsValid() {
		t.Fatal("root span id is invalid")
	}
	if tool.Parent.SpanID() != root.SpanContext.SpanID() {
		t.Error("execute_tool span is not a child of the invoke_agent span")
	}
}

func TestHallucinatedFieldsAreRejected(t *testing.T) {
	dir := t.TempDir()
	payload := writePayload(t, dir, basePayload())
	m := &fakeModel{replies: []Reply{{
		Model: "m",
		Text: `{"proposals":[
			{"source_field":"usage.inputTokens","target_field":"gen_ai.usage.input_tokens","relationship":"EXACT","confidence":0.99},
			{"source_field":"totally.invented.field","target_field":"gen_ai.usage.output_tokens","relationship":"EXACT","confidence":0.99}
		]}`,
	}}}

	res, err := Run(context.Background(), m, Options{System: "acme", PayloadPath: payload, OutDir: dir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, p := range res.Proposals {
		if p.SourceField == "totally.invented.field" {
			t.Fatal("a proposal for a field absent from the payload must be dropped")
		}
	}
}

func TestUndecidedFieldsBecomeUnknown(t *testing.T) {
	dir := t.TempDir()
	payload := writePayload(t, dir, basePayload())
	// The model only ever addresses one of the two fields.
	m := &fakeModel{replies: []Reply{{
		Model: "m",
		Text:  `{"proposals":[{"source_field":"model","target_field":"gen_ai.request.model","relationship":"EXACT","confidence":0.99}]}`,
	}}}

	res, err := Run(context.Background(), m, Options{System: "acme", PayloadPath: payload, OutDir: dir, MaxIterations: 1})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Proposals) != 2 {
		t.Fatalf("every source field must appear in the result, got %d: %+v", len(res.Proposals), res.Proposals)
	}
	var found bool
	for _, p := range res.Proposals {
		if p.SourceField == "usage.inputTokens" {
			found = true
			if p.Relationship != mapping.Unknown {
				t.Errorf("undecided field = %q, want UNKNOWN", p.Relationship)
			}
		}
	}
	if !found {
		t.Error("the undecided field is missing from the result entirely")
	}
	if !res.RequiresHumanReview {
		t.Error("a result containing UNKNOWN must require human review")
	}
}

func TestInvalidRelationshipDowngradesToUnknown(t *testing.T) {
	dir := t.TempDir()
	payload := writePayload(t, dir, basePayload())
	m := &fakeModel{replies: []Reply{{
		Model: "m",
		Text:  `{"proposals":[{"source_field":"model","target_field":"gen_ai.request.model","relationship":"DEFINITELY_FINE","confidence":1}]}`,
	}}}

	res, err := Run(context.Background(), m, Options{System: "acme", PayloadPath: payload, OutDir: dir, MaxIterations: 1})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, p := range res.Proposals {
		if p.SourceField == "model" {
			if p.Relationship != mapping.Unknown {
				t.Errorf("relationship = %q, want UNKNOWN for an unrecognized value", p.Relationship)
			}
			if p.TargetField != "" {
				t.Errorf("an UNKNOWN proposal must not keep a target field, got %q", p.TargetField)
			}
		}
	}
}

func TestArtifactsAreWritten(t *testing.T) {
	dir := t.TempDir()
	payload := writePayload(t, dir, basePayload())
	m := &fakeModel{replies: []Reply{okReply()}}

	res, err := Run(context.Background(), m, Options{System: "acme", PayloadPath: payload, OutDir: dir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := os.Stat(res.FixturePath); err != nil {
		t.Errorf("fixture not written: %v", err)
	}
	if _, err := os.Stat(res.ProposalPath); err != nil {
		t.Errorf("proposal not written: %v", err)
	}
}

func TestMalformedResponseIsRetriedNotFatal(t *testing.T) {
	dir := t.TempDir()
	payload := writePayload(t, dir, basePayload())
	m := &fakeModel{replies: []Reply{
		{Model: "m", Text: "I'm afraid I can't do that."},
		okReply(),
	}}

	res, err := Run(context.Background(), m, Options{System: "acme", PayloadPath: payload, OutDir: dir, MaxIterations: 3})
	if err != nil {
		t.Fatalf("a malformed model response must not fail the run: %v", err)
	}
	if m.calls < 2 {
		t.Errorf("expected a retry after unparseable output, got %d call(s)", m.calls)
	}
	if len(res.Proposals) != 2 {
		t.Errorf("expected recovery on the second turn, got %+v", res.Proposals)
	}
}

func TestMissingPayloadIsAnError(t *testing.T) {
	m := &fakeModel{replies: []Reply{okReply()}}
	if _, err := Run(context.Background(), m, Options{System: "acme", PayloadPath: "/nonexistent/x.json"}); err == nil {
		t.Fatal("expected an error for a missing payload file")
	}
}

func spanNames(spans tracetest.SpanStubs) []string {
	out := make([]string, 0, len(spans))
	for _, s := range spans {
		out = append(out, s.Name)
	}
	return out
}

// Nova returned "confidence": "HIGH" - a string where a number was requested.
// Before Confidence.UnmarshalJSON existed, that one type mismatch failed the
// whole document and every field fell through to UNKNOWN.
func TestQualitativeConfidenceIsAccepted(t *testing.T) {
	dir := t.TempDir()
	payload := writePayload(t, dir, basePayload())
	m := &fakeModel{replies: []Reply{{
		Model: "amazon.nova-lite-v1:0",
		Text: "```json\n{\"proposals\":[" +
			`{"source_field":"model","target_field":"gen_ai.request.model","relationship":"EXACT","confidence":"HIGH"},` +
			`{"source_field":"usage.inputTokens","target_field":"gen_ai.usage.input_tokens","relationship":"EXACT","confidence":"HIGH"}` +
			"]}\n```",
	}}}

	res, err := Run(context.Background(), m, Options{System: "acme", PayloadPath: payload, OutDir: dir, MaxIterations: 1})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, p := range res.Proposals {
		if p.Relationship == mapping.Unknown {
			t.Fatalf("field %q fell through to UNKNOWN despite a usable proposal", p.SourceField)
		}
	}
	// A qualitative label must not be able to auto-approve a mapping.
	if !res.RequiresHumanReview {
		t.Error(`"HIGH" is not a calibrated confidence and must still require review`)
	}
}

func TestConfidenceParsing(t *testing.T) {
	cases := map[string]Confidence{
		`0.82`:     0.82,
		`"0.82"`:   0.82,
		`"HIGH"`:   confidenceHigh,
		`"medium"`: confidenceMedium,
		`"Low"`:    confidenceLow,
		`"banana"`: 0,
		`null`:     0,
	}
	for in, want := range cases {
		var c Confidence
		if err := c.UnmarshalJSON([]byte(in)); err != nil {
			t.Errorf("UnmarshalJSON(%s) returned error: %v", in, err)
			continue
		}
		if c != want {
			t.Errorf("UnmarshalJSON(%s) = %v, want %v", in, c, want)
		}
	}
}

// Observed from a real Nova run: the model proposed llm.model.name,
// llm.usage.prompt_tokens, trace.id and gen_ai.usage.total_tokens at
// confidence 0.95. All are plausible and none exist in semconv 1.41.0.
func TestInventedTargetAttributesAreRejected(t *testing.T) {
	dir := t.TempDir()
	payload := writePayload(t, dir, basePayload())
	m := &fakeModel{replies: []Reply{{
		Model: "m",
		Text: `{"proposals":[
			{"source_field":"model","target_field":"llm.model.name","relationship":"EXACT","confidence":0.95},
			{"source_field":"usage.inputTokens","target_field":"gen_ai.usage.input_tokens","relationship":"EXACT","confidence":0.95}
		]}`,
	}}}

	res, err := Run(context.Background(), m, Options{System: "acme", PayloadPath: payload, OutDir: dir, MaxIterations: 1})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, p := range res.Proposals {
		switch p.SourceField {
		case "model":
			if p.Relationship != mapping.Unknown {
				t.Errorf("llm.model.name is not a semconv attribute and must be rejected, got %q", p.Relationship)
			}
			if p.TargetField != "" {
				t.Errorf("a rejected proposal must not keep its target, got %q", p.TargetField)
			}
		case "usage.inputTokens":
			if p.Relationship != mapping.Exact {
				t.Errorf("a valid semconv target must survive, got %q", p.Relationship)
			}
		}
	}
}
