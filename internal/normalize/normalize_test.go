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
