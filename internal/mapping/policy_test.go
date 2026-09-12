package mapping

import "testing"

func TestAggregateExactHighConfidenceCanBecomeCandidate(t *testing.T) {
	req := MappingRequest{ID: "m1"}
	as := []AgentAssessment{
		{Agent: "semantic", Relationship: Exact, Confidence: .98},
		{Agent: "otel", Relationship: Exact, Confidence: .96},
		{Agent: "loss", Relationship: Exact, Confidence: .95},
	}
	reviewer := &AgentAssessment{Agent: "reviewer", Relationship: Exact, Confidence: .95}
	got := Aggregate(req, as, reviewer)
	if got.RequiresHumanReview || got.Decision != "CANDIDATE_APPROVAL" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestAggregateUnknownAlwaysReviews(t *testing.T) {
	req := MappingRequest{ID: "m2"}
	as := []AgentAssessment{{Agent: "semantic", Relationship: Unknown, Confidence: .7}, {Agent: "otel", Relationship: Compatible, Confidence: .9}, {Agent: "loss", Relationship: Lossy, Confidence: .8}}
	got := Aggregate(req, as, nil)
	if !got.RequiresHumanReview || got.Decision != "REVIEW" {
		t.Fatalf("unexpected: %+v", got)
	}
}
