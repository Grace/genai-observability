package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/grace/genai-observability/internal/lambdatel"
	"github.com/grace/genai-observability/internal/model"
	"go.opentelemetry.io/otel/attribute"
)

type Input struct {
	Request   model.EvalRequest  `json:"request"`
	Results   []model.EvalResult `json:"results"`
	Consensus model.Consensus    `json:"consensus"`
}

var s3c *s3.Client
var ddb *dynamodb.Client

func init() {
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		panic(err)
	}
	s3c = s3.NewFromConfig(cfg)
	ddb = dynamodb.NewFromConfig(cfg)
}

func handler(ctx context.Context, in Input) (model.HarnessOutput, error) {
	_ = lambdatel.Setup(ctx, "genai-observability-normalizer")
	ctx, span := lambdatel.Start(ctx, in.Request.TraceContext, "evaluation.persist")
	defer func() { span.End(); _ = lambdatel.Flush(ctx) }()
	span.SetAttributes(
		attribute.String("trace.reference", in.Request.TraceID),
		attribute.String("gen_ai.request.model", in.Request.Model),
		attribute.String("prompt.version", in.Request.PromptVersion),
		attribute.Int64("gen_ai.usage.input_tokens", in.Request.InputTokens),
		attribute.Int64("gen_ai.usage.output_tokens", in.Request.OutputTokens),
		attribute.Float64("gen_ai.response.latency_ms", in.Request.LatencyMS),
		attribute.Float64("evaluation.mean", in.Consensus.MeanScore),
		attribute.Float64("evaluation.judge_agreement", in.Consensus.JudgeAgreement),
	)
	if in.Request.CostUSD != nil {
		span.SetAttributes(attribute.Float64("gen_ai.usage.cost_usd", *in.Request.CostUSD), attribute.String("gen_ai.usage.pricing_version", in.Request.PricingVersion))
	}

	out := model.HarnessOutput{Request: in.Request, Results: in.Results, Consensus: in.Consensus}
	payload, _ := json.MarshalIndent(out, "", "  ")
	bucket := os.Getenv("CORPUS_BUCKET")
	if bucket != "" {
		keys := []string{fmt.Sprintf("evaluations/%s.json", in.Request.TraceID)}
		if _, replay := in.Request.Attributes["replay.of_trace_id"]; !replay {
			keys = append(keys, fmt.Sprintf("corpus/production/%s.json", in.Request.TraceID))
		}
		for _, key := range keys {
			if _, err := s3c.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String(key), Body: strings.NewReader(string(payload)), ContentType: aws.String("application/json")}); err != nil {
				return out, err
			}
		}
	}
	if table := os.Getenv("EVAL_TABLE"); table != "" {
		b, _ := json.Marshal(out)
		_, err := ddb.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String(table), Item: map[string]ddbtypes.AttributeValue{
			"pk":              &ddbtypes.AttributeValueMemberS{Value: "TRACE#" + in.Request.TraceID},
			"sk":              &ddbtypes.AttributeValueMemberS{Value: "EVAL#" + time.Now().UTC().Format(time.RFC3339Nano)},
			"payload":         &ddbtypes.AttributeValueMemberS{Value: string(b)},
			"judge_agreement": &ddbtypes.AttributeValueMemberN{Value: fmt.Sprintf("%.6f", in.Consensus.JudgeAgreement)},
		}})
		if err != nil {
			return out, err
		}
	}
	if baselineTraceID, replay := in.Request.Attributes["replay.of_trace_id"]; replay && bucket != "" {
		comparison, err := buildReplayComparison(ctx, bucket, baselineTraceID, out)
		if err != nil {
			span.RecordError(err)
		} else {
			span.SetAttributes(
				attribute.Float64("replay.quality_delta", comparison.QualityDelta),
				attribute.Float64("replay.latency_delta_ms", comparison.LatencyDeltaMS),
			)
			if comparison.CostDeltaUSD != nil {
				span.SetAttributes(attribute.Float64("replay.cost_delta_usd", *comparison.CostDeltaUSD))
			}
			cb, _ := json.MarshalIndent(comparison, "", "  ")
			key := fmt.Sprintf("replay-comparisons/%s.json", in.Request.TraceID)
			if _, err := s3c.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String(key), Body: strings.NewReader(string(cb)), ContentType: aws.String("application/json")}); err != nil {
				return out, err
			}
		}
	}
	return out, nil
}

func buildReplayComparison(ctx context.Context, bucket, baselineTraceID string, replay model.HarnessOutput) (model.ReplayComparison, error) {
	obj, err := s3c.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String("evaluations/" + baselineTraceID + ".json")})
	if err != nil {
		return model.ReplayComparison{}, err
	}
	defer obj.Body.Close()
	raw, err := io.ReadAll(obj.Body)
	if err != nil {
		return model.ReplayComparison{}, err
	}
	var baseline model.HarnessOutput
	if err := json.Unmarshal(raw, &baseline); err != nil {
		return model.ReplayComparison{}, err
	}
	c := model.ReplayComparison{
		BaselineTraceID:   baselineTraceID,
		ReplayTraceID:     replay.Request.TraceID,
		BaselineModel:     baseline.Request.Model,
		ReplayModel:       replay.Request.Model,
		QualityBaseline:   baseline.Consensus.MeanScore,
		QualityReplay:     replay.Consensus.MeanScore,
		QualityDelta:      replay.Consensus.MeanScore - baseline.Consensus.MeanScore,
		CostBaselineUSD:   baseline.Request.CostUSD,
		CostReplayUSD:     replay.Request.CostUSD,
		LatencyBaselineMS: baseline.Request.LatencyMS,
		LatencyReplayMS:   replay.Request.LatencyMS,
		LatencyDeltaMS:    replay.Request.LatencyMS - baseline.Request.LatencyMS,
		PricingVersion:    replay.Request.PricingVersion,
		ObservedAt:        time.Now().UTC(),
	}
	if baseline.Request.CostUSD != nil && replay.Request.CostUSD != nil {
		d := *replay.Request.CostUSD - *baseline.Request.CostUSD
		c.CostDeltaUSD = &d
	}
	return c, nil
}

func main() { lambda.Start(handler) }
