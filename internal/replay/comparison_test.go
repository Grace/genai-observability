package replay

import "testing"

func TestCompare(t *testing.T) {
	baseCost, candidateCost := 0.021, 0.014
	got := Compare(Baseline{LatencyMS: 920, CostUSD: &baseCost}, Candidate{LatencyMS: 710, CostUSD: &candidateCost})
	if got.LatencyMS != -210 {
		t.Fatalf("latency delta=%v", got.LatencyMS)
	}
	if got.CostUSD == nil || *got.CostUSD > -0.006999 || *got.CostUSD < -0.007001 {
		t.Fatalf("cost delta=%v", got.CostUSD)
	}
}
