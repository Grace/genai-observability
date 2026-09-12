package main

import (
	"os"
	"regexp"
	"testing"

	"github.com/grace/genai-observability/internal/canonical"
	"github.com/grace/genai-observability/internal/semconv"
	"go.opentelemetry.io/otel/attribute"
)

func find(attrs []attribute.KeyValue, key string) (attribute.Value, bool) {
	for _, kv := range attrs {
		if string(kv.Key) == key {
			return kv.Value, true
		}
	}
	return attribute.Value{}, false
}

func TestIssueAttributesEmitsCodesAndFields(t *testing.T) {
	issues := []canonical.Issue{
		{Code: "usage_mismatch", Field: "total_tokens", Message: "reported total disagrees with sum"},
		{Code: "unknown_score_semantics", Field: "score", Message: "scalar semantics unknown"},
	}
	attrs := issueAttributes("warning", issues)

	codes, ok := find(attrs, "normalization.warning.codes")
	if !ok {
		t.Fatal("expected normalization.warning.codes to be emitted")
	}
	// Sorted, so a given set of issues always produces the same attribute value.
	got := codes.AsStringSlice()
	want := []string{"unknown_score_semantics", "usage_mismatch"}
	if len(got) != len(want) {
		t.Fatalf("codes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("codes = %v, want %v", got, want)
		}
	}

	fields, ok := find(attrs, "normalization.warning.fields")
	if !ok {
		t.Fatal("expected normalization.warning.fields to be emitted")
	}
	if len(fields.AsStringSlice()) != 2 {
		t.Fatalf("fields = %v, want 2 entries", fields.AsStringSlice())
	}
}

func TestIssueAttributesDeduplicates(t *testing.T) {
	issues := []canonical.Issue{
		{Code: "lossy_mapping", Field: "input_tokens"},
		{Code: "lossy_mapping", Field: "input_tokens"},
		{Code: "lossy_mapping", Field: "output_tokens"},
	}
	attrs := issueAttributes("warning", issues)

	codes, _ := find(attrs, "normalization.warning.codes")
	if n := len(codes.AsStringSlice()); n != 1 {
		t.Fatalf("expected 1 distinct code, got %d: %v", n, codes.AsStringSlice())
	}
	fields, _ := find(attrs, "normalization.warning.fields")
	if n := len(fields.AsStringSlice()); n != 2 {
		t.Fatalf("expected 2 distinct fields, got %d: %v", n, fields.AsStringSlice())
	}
}

func TestIssueAttributesEmptyEmitsNothing(t *testing.T) {
	if attrs := issueAttributes("warning", nil); attrs != nil {
		t.Fatalf("expected no attributes for zero issues, got %v", attrs)
	}
}

func TestIssueAttributesSkipsBlankFields(t *testing.T) {
	// Not every issue carries a field; a blank one must not become an empty
	// dimension value, which would pollute grouping in the backend.
	issues := []canonical.Issue{{Code: "missing_operation"}}
	attrs := issueAttributes("error", issues)

	if _, ok := find(attrs, "normalization.error.codes"); !ok {
		t.Fatal("expected codes to be emitted")
	}
	if _, ok := find(attrs, "normalization.error.fields"); ok {
		t.Fatal("expected no fields attribute when no issue carries a field")
	}
}

// semconv 1.41.0 defines gen_ai.usage.input_tokens and output_tokens but no
// total. Emitting a total under the gen_ai namespace would invent an attribute
// in a namespace this service does not own - the same defect it reports on its
// inputs - so the total is carried under telemetry.* instead.
func TestNoInventedGenAIAttributesAreEmitted(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	// Only attribute keys, not span names: a span may legitimately be called
	// "gen_ai.request" without that being an attribute claim.
	re := regexp.MustCompile(`attribute\.[A-Za-z0-9]+\("(gen_ai\.[a-z0-9_.]+)"`)
	for _, m := range re.FindAllStringSubmatch(string(src), -1) {
		name := m[1]
		if semconv.Known(name) {
			continue
		}
		if semconv.Extension(name) {
			// Declared, with a reason. Allowed, and visible in review.
			continue
		}
		t.Errorf("main.go emits %q, which is neither defined in semantic conventions %s nor declared as an extension in internal/semconv", name, semconv.Version)
	}
}
