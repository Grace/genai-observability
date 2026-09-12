package main

import (
	"context"
	"strings"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/grace/genai-observability/internal/model"
)

func handler(ctx context.Context, r model.EvalRequest) (model.EvalResult, error) {
	pass := strings.TrimSpace(r.Answer) != ""
	v := 0.0
	if pass {
		v = 1
	}
	return model.EvalResult{
		Name: "schema_valid", Kind: "boolean", Value: v, Pass: &pass,
		Reason:    "answer must be non-empty",
		Evaluator: model.Evaluator{Type: "deterministic", Version: "v1"},
		Semantics: model.EvalSemantics{Concept: "schema_validity", ScaleMin: 0, ScaleMax: 1, HigherIsBetter: true},
		TraceID:   r.TraceID,
	}, nil
}

func main() { lambda.Start(handler) }
