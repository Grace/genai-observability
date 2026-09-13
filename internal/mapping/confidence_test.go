package mapping

import (
	"encoding/json"
	"testing"
)

// The deployed adversarial reviewer returned "confidence": "HIGH", which failed
// the whole assessment and recorded the agent as UNKNOWN. The aggregation policy
// then correctly demanded human review, so the system stayed safe while one of
// its four agents was silently dead.
func TestQualitativeConfidenceDoesNotBreakAnAssessment(t *testing.T) {
	var a struct {
		Relationship Relationship `json:"relationship"`
		Confidence   Confidence   `json:"confidence"`
	}
	body := `{"relationship":"EXACT","confidence":"HIGH"}`
	if err := json.Unmarshal([]byte(body), &a); err != nil {
		t.Fatalf("a qualitative confidence must not fail the decode: %v", err)
	}
	if a.Relationship != Exact {
		t.Errorf("relationship = %q, want EXACT", a.Relationship)
	}
	if a.Confidence != ConfidenceHigh {
		t.Errorf("confidence = %v, want %v", a.Confidence, ConfidenceHigh)
	}
	// A label must not clear the auto-approval bar on its own.
	if a.Confidence >= 0.90 {
		t.Error("a qualitative label must stay below the auto-approval threshold")
	}
}

func TestConfidenceForms(t *testing.T) {
	cases := map[string]Confidence{
		`0.82`:      0.82,
		`"0.82"`:    0.82,
		`"HIGH"`:    ConfidenceHigh,
		`"medium"`:  ConfidenceMedium,
		`"Low"`:     ConfidenceLow,
		`"unknown"`: 0,
		`null`:      0,
		`true`:      0,
	}
	for in, want := range cases {
		var c Confidence
		if err := c.UnmarshalJSON([]byte(in)); err != nil {
			t.Errorf("UnmarshalJSON(%s): %v", in, err)
			continue
		}
		if c != want {
			t.Errorf("UnmarshalJSON(%s) = %v, want %v", in, c, want)
		}
	}
}

// A reviewer that fails to parse must still force human review, which is the
// behaviour that contained the live defect.
func TestUnknownReviewerForcesHumanReview(t *testing.T) {
	req := MappingRequest{ID: "t"}
	assessments := []AgentAssessment{
		{Agent: "semantic", Relationship: Exact, Confidence: 0.95},
		{Agent: "otel", Relationship: Exact, Confidence: 0.95},
	}
	reviewer := &AgentAssessment{Agent: "reviewer", Relationship: Unknown, Confidence: 0}
	out := Aggregate(req, assessments, reviewer)
	if !out.RequiresHumanReview {
		t.Error("an UNKNOWN reviewer must force human review")
	}
	if out.Decision != "REVIEW" {
		t.Errorf("decision = %q, want REVIEW", out.Decision)
	}
}
