package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/grace/genai-observability/internal/lambdatel"
	"github.com/grace/genai-observability/internal/mapping"
	"go.opentelemetry.io/otel/attribute"
)

type input struct {
	Request     mapping.MappingRequest    `json:"request"`
	Assessments []mapping.AgentAssessment `json:"assessments"`
	Reviewer    mapping.AgentAssessment   `json:"reviewer"`
}

func handler(ctx context.Context, in input) (mapping.MappingAssessment, error) {
	if err := lambdatel.Setup(ctx, "genai-observability-mapping-aggregate"); err != nil {
		slog.Error("telemetry unavailable for this invocation", "err", err)
	}
	ctx, span := lambdatel.Start(ctx, in.Request.TraceContext, "mapping.aggregate")
	defer func() {
		span.End()
		if err := lambdatel.Flush(ctx); err != nil {
			slog.Error("span flush failed", "err", err)
		}
	}()
	out := mapping.Aggregate(in.Request, in.Assessments, &in.Reviewer)
	out.CreatedAt = time.Now().UTC()
	span.SetAttributes(
		attribute.String("mapping.relationship", string(out.Relationship)),
		attribute.Float64("mapping.confidence", out.Confidence),
		attribute.Bool("mapping.requires_human_review", out.RequiresHumanReview),
		attribute.String("mapping.decision", out.Decision),
	)
	return out, nil
}

func main() { lambda.Start(handler) }
