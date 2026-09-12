package replay

type Baseline struct {
	LatencyMS float64
	CostUSD   *float64
}

type Candidate struct {
	LatencyMS float64
	CostUSD   *float64
}

type Delta struct {
	LatencyMS float64  `json:"latency_delta_ms"`
	CostUSD   *float64 `json:"cost_delta_usd,omitempty"`
}

func Compare(base Baseline, candidate Candidate) Delta {
	out := Delta{LatencyMS: candidate.LatencyMS - base.LatencyMS}
	if base.CostUSD != nil && candidate.CostUSD != nil {
		d := *candidate.CostUSD - *base.CostUSD
		out.CostUSD = &d
	}
	return out
}
