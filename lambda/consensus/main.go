package main

import (
	"context"
	"log/slog"
	"math"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/grace/genai-observability/internal/lambdatel"
	"github.com/grace/genai-observability/internal/model"
	"go.opentelemetry.io/otel/attribute"
)

func handler(ctx context.Context, results []model.EvalResult) (model.Consensus, error) {
	if err := lambdatel.Setup(ctx, "genai-observability-consensus"); err != nil {
		slog.Error("telemetry unavailable for this invocation", "err", err)
	}
	var carrier map[string]string
	if len(results) > 0 {
		carrier = results[0].TraceContext
	}
	ctx, span := lambdatel.Start(ctx, carrier, "evaluation.consensus")
	defer func() {
		span.End()
		if err := lambdatel.Flush(ctx); err != nil {
			slog.Error("span flush failed", "err", err)
		}
	}()
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
	span.SetAttributes(attribute.Float64("evaluation.mean", mean), attribute.Float64("evaluation.judge_agreement", agreement), attribute.Int("evaluation.judge_count", len(scores)))
	return model.Consensus{MeanScore: mean, StdDev: stddev, JudgeAgreement: agreement, MaxDisagreement: maxDisagreement, JudgeCount: len(scores)}, nil
}

func main() { lambda.Start(handler) }
