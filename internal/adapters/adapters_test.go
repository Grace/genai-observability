package adapters

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/grace/genai-observability/internal/canonical"
)

// Fixtures are normalize.Request documents: the adapter receives the payload,
// not the envelope. Unwrapping here mirrors normalize.Run, which cannot be
// imported from this package because it imports adapters.
func load(t *testing.T, fixture string) (source string, payload json.RawMessage) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "examples", "normalization", fixture+".json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var env struct {
		Source  string          `json:"source"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	if len(env.Payload) == 0 {
		t.Fatalf("fixture %s has no payload", fixture)
	}
	return env.Source, env.Payload
}

func codes(issues []canonical.Issue) []string {
	out := make([]string, 0, len(issues))
	for _, i := range issues {
		out = append(out, i.Code)
	}
	return out
}

func has(issues []canonical.Issue, code string) bool {
	for _, i := range issues {
		if i.Code == code {
			return true
		}
	}
	return false
}

// The seven shipped fixtures back README claim 1 and were previously exercised
// by no Go test at all - only indirectly through the CLI.
func TestEveryShippedFixtureNormalizes(t *testing.T) {
	cases := []struct {
		fixture  string
		source   string
		provider string
		wantIn   int64
		wantOut  int64
	}{
		{"otel", "opentelemetry", "aws.bedrock", 128, 42},
		{"openllmetry", "openllmetry", "anthropic", 80, 20},
		{"braintrust", "braintrust", "", 0, 0},
		{"bedrock", "aws-bedrock", "aws.bedrock", 0, 0},
		{"pydanticai", "pydantic-ai", "", 0, 0},
		{"eino", "eino", "", 0, 0},
		{"genkit", "genkit", "", 125, 61},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			source, payload := load(t, tc.fixture)
			ad, err := For(source)
			if err != nil {
				t.Fatalf("no adapter for %q: %v", source, err)
			}
			if ad.Name() != tc.source {
				t.Errorf("adapter name = %q, want %q", ad.Name(), tc.source)
			}
			r, err := ad.Normalize(payload)
			if err != nil {
				t.Fatalf("normalize: %v", err)
			}
			if len(r.Errors) > 0 {
				t.Fatalf("fixture produced errors: %v", codes(r.Errors))
			}
			if tc.provider != "" && r.Record.Provider != tc.provider {
				t.Errorf("provider = %q, want %q", r.Record.Provider, tc.provider)
			}
			if tc.wantIn != 0 && r.Record.InputTokens != tc.wantIn {
				t.Errorf("input tokens = %d, want %d", r.Record.InputTokens, tc.wantIn)
			}
			if tc.wantOut != 0 && r.Record.OutputTokens != tc.wantOut {
				t.Errorf("output tokens = %d, want %d", r.Record.OutputTokens, tc.wantOut)
			}
			// No fixture should trip the ambiguity reporters.
			for _, bad := range []string{"alias_conflict", "non_integer_token_count"} {
				if has(r.Warnings, bad) {
					t.Errorf("shipped fixture unexpectedly produced %s: %v", bad, codes(r.Warnings))
				}
			}
		})
	}
}

// Two aliases disagreeing about the same concept is the ambiguity this project
// exists to surface. It was previously decided by argument order, silently.
func TestConflictingAliasesAreReported(t *testing.T) {
	m := map[string]any{
		"source": "opentelemetry",
		"attributes": map[string]any{
			"gen_ai.operation.name":      "chat",
			"gen_ai.usage.input_tokens":  10,
			"gen_ai.usage.prompt_tokens": 9999,
			"gen_ai.usage.output_tokens": 4,
			"gen_ai.provider.name":       "openai",
			"gen_ai.system":              "anthropic",
		},
	}
	r := normalizeOTel(m)

	if !has(r.Warnings, "alias_conflict") {
		t.Fatalf("expected alias_conflict, got %v", codes(r.Warnings))
	}
	// Precedence is unchanged: first listed still wins.
	if r.Record.InputTokens != 10 {
		t.Errorf("input tokens = %d, want 10 (first alias wins)", r.Record.InputTokens)
	}
	if r.Record.Provider != "openai" {
		t.Errorf("provider = %q, want openai", r.Record.Provider)
	}
	// Both the numeric and the string conflict should be reported.
	var n int
	for _, i := range r.Warnings {
		if i.Code == "alias_conflict" {
			n++
		}
	}
	if n < 2 {
		t.Errorf("expected a conflict for both tokens and provider, got %d", n)
	}
}

func TestMatchingAliasesAreNotAConflict(t *testing.T) {
	m := map[string]any{
		"attributes": map[string]any{
			"gen_ai.operation.name":      "chat",
			"gen_ai.usage.input_tokens":  10,
			"gen_ai.usage.prompt_tokens": 10,
		},
	}
	r := normalizeOTel(m)
	if has(r.Warnings, "alias_conflict") {
		t.Error("aliases carrying the same value must not be reported as conflicting")
	}
}

// A reported zero was previously overwritten by input+output, which erased the
// disagreement before normalize could see it. usage_mismatch only fires when the
// total is greater than zero, so the check could never catch this case.
func TestReportedZeroTotalIsPreserved(t *testing.T) {
	m := map[string]any{
		"attributes": map[string]any{
			"gen_ai.operation.name":      "chat",
			"gen_ai.usage.input_tokens":  10,
			"gen_ai.usage.output_tokens": 4,
			"gen_ai.usage.total_tokens":  0,
		},
	}
	r := normalizeOTel(m)
	if r.Record.TotalTokens != 0 {
		t.Errorf("total = %d, want 0 preserved so the disagreement survives", r.Record.TotalTokens)
	}
}

func TestAbsentTotalIsStillSynthesized(t *testing.T) {
	m := map[string]any{
		"attributes": map[string]any{
			"gen_ai.operation.name":      "chat",
			"gen_ai.usage.input_tokens":  10,
			"gen_ai.usage.output_tokens": 4,
		},
	}
	r := normalizeOTel(m)
	if r.Record.TotalTokens != 14 {
		t.Errorf("total = %d, want 14 synthesized when genuinely absent", r.Record.TotalTokens)
	}
}

// int64(10.9) == 10 discarded the signal that something upstream is wrong.
func TestFractionalTokenCountIsReported(t *testing.T) {
	m := map[string]any{
		"attributes": map[string]any{
			"gen_ai.operation.name":      "chat",
			"gen_ai.usage.input_tokens":  10.9,
			"gen_ai.usage.output_tokens": 4,
		},
	}
	r := normalizeOTel(m)
	if !has(r.Warnings, "non_integer_token_count") {
		t.Fatalf("expected non_integer_token_count, got %v", codes(r.Warnings))
	}
	if r.Record.InputTokens != 10 {
		t.Errorf("input tokens = %d, want 10 after truncation", r.Record.InputTokens)
	}
}

// Braintrust scores came from a map, so identical input produced different
// output orderings between runs. Evaluations are emitted with index-derived
// keys downstream, so ordering decides what each column means.
func TestBraintrustScoreOrderIsDeterministic(t *testing.T) {
	m := map[string]any{
		"scores": map[string]any{
			"helpfulness": 0.9,
			"accuracy":    0.8,
			"tone":        0.7,
			"grounding":   0.6,
			"relevance":   0.5,
		},
	}
	first := normalizeBraintrust(m).Record.Evaluations
	for i := 0; i < 50; i++ {
		got := normalizeBraintrust(m).Record.Evaluations
		if !reflect.DeepEqual(first, got) {
			t.Fatalf("evaluation order varies between runs:\nfirst %v\ngot   %v", first, got)
		}
	}
	if len(first) != 5 {
		t.Fatalf("expected 5 evaluations, got %d", len(first))
	}
	if first[0].Name != "accuracy" {
		t.Errorf("first evaluation = %q, want accuracy (sorted)", first[0].Name)
	}
}

func TestUnknownSourceIsAnError(t *testing.T) {
	if _, err := For("definitely-not-a-framework"); err == nil {
		t.Fatal("expected an error for an unknown source system")
	}
}
