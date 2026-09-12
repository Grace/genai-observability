package main

import (
	"context"
	"math"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/grace/genai-observability/internal/model"
)

func handler(ctx context.Context, results []model.EvalResult) (model.Consensus, error) {
	var scores []float64
	for _, r := range results {
		if r.Evaluator.Type == "llm_judge" && r.Semantics.Concept == "groundedness" {
			scores = append(scores, r.Value)
		}
	}
	if len(scores) == 0 {
		return model.Consensus{}, nil
	}
	mean := 0.0
	for _, v := range scores {
		mean += v
	}
	mean /= float64(len(scores))
	variance := 0.0
	minV, maxV := scores[0], scores[0]
	for _, v := range scores {
		d := v - mean
		variance += d * d
		if v < minV {
			minV = v
		}
		if v > maxV {
			maxV = v
		}
	}
	variance /= float64(len(scores))
	stddev := math.Sqrt(variance)
	maxDisagreement := maxV - minV
	agreement := 1 - maxDisagreement
	if agreement < 0 {
		agreement = 0
	}
	return model.Consensus{MeanScore: mean, StdDev: stddev, JudgeAgreement: agreement, MaxDisagreement: maxDisagreement, JudgeCount: len(scores)}, nil
}

func main() { lambda.Start(handler) }
