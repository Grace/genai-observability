package normalize

import (
	"encoding/json"
	"testing"
)

func TestOTelNormalization(t *testing.T) {
	req := Request{Source: "otel", Payload: json.RawMessage(`{"trace_id":"abc","attributes":{"gen_ai.operation.name":"chat","gen_ai.provider.name":"aws.bedrock","gen_ai.request.model":"m1","gen_ai.usage.input_tokens":10,"gen_ai.usage.output_tokens":4}}`)}
	r, err := Run(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Errors) != 0 {
		t.Fatalf("unexpected errors: %+v", r.Errors)
	}
	if r.Record.TotalTokens != 14 {
		t.Fatalf("want 14 tokens, got %d", r.Record.TotalTokens)
	}
	if r.Record.Provider != "aws.bedrock" {
		t.Fatalf("provider=%q", r.Record.Provider)
	}
}

func TestUnknownScoreDoesNotInventProbabilitySemantics(t *testing.T) {
	req := Request{Source: "braintrust", Payload: json.RawMessage(`{"name":"llm","scores":{"groundedness":0.91}}`)}
	r, err := Run(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Record.Evaluations) != 1 {
		t.Fatal("expected score")
	}
	if r.Record.Evaluations[0].Kind != "scalar" {
		t.Fatalf("kind=%s", r.Record.Evaluations[0].Kind)
	}
	if len(r.Warnings) == 0 {
		t.Fatal("expected semantic warning")
	}
}

func TestTokenMismatchIsVisible(t *testing.T) {
	req := Request{Source: "otel", Payload: json.RawMessage(`{"attributes":{"gen_ai.operation.name":"chat","gen_ai.usage.input_tokens":10,"gen_ai.usage.output_tokens":4,"gen_ai.usage.total_tokens":20}}`)}
	r, err := Run(req)
	if err != nil {
		t.Fatal(err)
	}
	if r.Record.TotalTokens != 20 {
		t.Fatal("normalizer should preserve reported value")
	}
	if len(r.Warnings) == 0 {
		t.Fatal("expected mismatch warning")
	}
}

func TestFrameworkAdaptersNormalizeEquivalentInvocation(t *testing.T) {
	cases := []Request{
		{Source: "pydanticai", Payload: []byte(`{"attributes":{"gen_ai.operation.name":"chat","gen_ai.provider.name":"openai","gen_ai.request.model":"gpt-4.1","gen_ai.usage.input_tokens":125,"gen_ai.usage.output_tokens":61}}`)},
		{Source: "eino", Payload: []byte(`{"component":"ChatModel","provider":"openai","model":"gpt-4.1","input_tokens":125,"output_tokens":61}`)},
		{Source: "genkit", Payload: []byte(`{"span_type":"generate","model":"openai/gpt-4.1","usage":{"inputTokens":125,"outputTokens":61}}`)},
	}
	for _, tc := range cases {
		r, err := Run(tc)
		if err != nil {
			t.Fatalf("%s: %v", tc.Source, err)
		}
		if r.Record.Operation != "chat" || r.Record.Provider != "openai" || r.Record.Model != "gpt-4.1" || r.Record.TotalTokens != 186 {
			t.Fatalf("%s normalized unexpectedly: %+v", tc.Source, r.Record)
		}
	}
}

// A source reporting total_tokens: 0 alongside non-zero input and output is
// disagreeing with itself. The adapters used to overwrite that zero with
// input+output, so the comparison here saw 14 == 14 and the disagreement was
// gone before this check ran. The evidence is now preserved end to end.
func TestReportedZeroTotalSurfacesAsUsageMismatch(t *testing.T) {
	req := Request{
		Source:  "otel",
		Payload: []byte(`{"attributes":{"gen_ai.operation.name":"chat","gen_ai.usage.input_tokens":10,"gen_ai.usage.output_tokens":4,"gen_ai.usage.total_tokens":0}}`),
	}
	r, err := Run(req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var found bool
	for _, w := range r.Warnings {
		if w.Code == "usage_mismatch" {
			found = true
		}
	}
	if !found {
		var got []string
		for _, w := range r.Warnings {
			got = append(got, w.Code)
		}
		t.Fatalf("expected usage_mismatch for a reported zero total, got %v", got)
	}
	if r.Record.TotalTokens != 0 {
		t.Errorf("total = %d, want the reported 0 preserved", r.Record.TotalTokens)
	}
}
