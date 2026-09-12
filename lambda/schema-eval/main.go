package main

import (
	"context"
	"log/slog"
	"strings"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/grace/genai-observability/internal/lambdatel"
	"github.com/grace/genai-observability/internal/model"
	"go.opentelemetry.io/otel/attribute"
)

func handler(ctx context.Context, r model.EvalRequest) (model.EvalResult, error) {
	if err := lambdatel.Setup(ctx, "genai-observability-schema-eval"); err != nil {
		slog.Error("telemetry unavailable for this invocation", "err", err)
	}
	ctx, span := lambdatel.Start(ctx, r.TraceContext, "evaluation.schema")
	defer func() {
		span.End()
		if err := lambdatel.Flush(ctx); err != nil {
			slog.Error("span flush failed", "err", err)
		}
	}()
	span.SetAttributes(attribute.String("gen_ai.request.model", r.Model), attribute.String("trace.reference", r.TraceID))
	pass := strings.TrimSpace(r.Answer) != ""
	v := 0.0
	if pass {
		v = 1
	}
	return model.EvalResult{
		Name: "schema_valid", Kind: "boolean", Value: v, Pass: &pass,
		Reason:       "answer must be non-empty",
		Evaluator:    model.Evaluator{Type: "deterministic", Version: "v1"},
		Semantics:    model.EvalSemantics{Concept: "schema_validity", ScaleMin: 0, ScaleMax: 1, HigherIsBetter: true},
		TraceID:      r.TraceID,
		TraceContext: r.TraceContext,
	}, nil
}

func main() { lambda.Start(handler) }
