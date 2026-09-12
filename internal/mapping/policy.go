package mapping

import "math"

const PolicyVersion = "mapping-policy-v1"

// Aggregate is deliberately conservative. Agent agreement is evidence, not truth.
// Anything uncertain, lossy, conflicting, or reviewer-challenged goes to a human.
func Aggregate(req MappingRequest, assessments []AgentAssessment, reviewer *AgentAssessment) MappingAssessment {
	out := MappingAssessment{ID: req.ID, Source: req.Source, Target: req.Target, AgentAssessments: assessments, Reviewer: reviewer, PolicyVersion: PolicyVersion, Decision: "REVIEW"}
	if len(assessments) == 0 {
		out.Relationship = Unknown
		out.RequiresHumanReview = true
		return out
	}

	counts := map[Relationship]int{}
	var conf float64
	for _, a := range assessments {
		counts[a.Relationship]++
		conf += clamp(a.Confidence)
		out.Evidence = append(out.Evidence, a.Evidence...)
		out.Objections = append(out.Objections, a.Objections...)
	}
	conf /= float64(len(assessments))
	winner, n := Unknown, 0
	for rel, c := range counts {
		if c > n {
			winner, n = rel, c
		}
	}
	out.Relationship = winner
	out.Confidence = math.Round(conf*1000) / 1000

	if reviewer != nil {
		out.Objections = append(out.Objections, reviewer.Objections...)
		if reviewer.Relationship == Conflicting || reviewer.Relationship == Unknown || reviewer.Confidence < 0.75 {
			out.RequiresHumanReview = true
			return out
		}
	}

	unanimous := n == len(assessments)
	safeRelationship := winner == Exact || winner == Compatible
	out.RequiresHumanReview = !(unanimous && safeRelationship && out.Confidence >= 0.90 && len(out.Objections) == 0)
	if !out.RequiresHumanReview {
		out.Decision = "CANDIDATE_APPROVAL"
	}
	return out
}

func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
