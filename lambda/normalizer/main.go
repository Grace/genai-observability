package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/grace/genai-observability/internal/model"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

type Input struct {
	Request   model.EvalRequest  `json:"request"`
	Results   []model.EvalResult `json:"results"`
	Consensus model.Consensus    `json:"consensus"`
}

var s3c *s3.Client
var ddb *dynamodb.Client
var sm *secretsmanager.Client

func init() {
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		panic(err)
	}
	s3c = s3.NewFromConfig(cfg)
	ddb = dynamodb.NewFromConfig(cfg)
	sm = secretsmanager.NewFromConfig(cfg)
}

func handler(ctx context.Context, in Input) (model.HarnessOutput, error) {
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
	_ = exportHoneycomb(ctx, out)
	return out, nil
}

func exportHoneycomb(ctx context.Context, out model.HarnessOutput) error {
	key := os.Getenv("HONEYCOMB_API_KEY")
	if key == "" {
		if arn := os.Getenv("HONEYCOMB_API_KEY_SECRET_ARN"); arn != "" {
			sec, e := sm.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: aws.String(arn)})
			if e == nil && sec.SecretString != nil {
				key = *sec.SecretString
			}
		}
	}
	if key == "" {
		return nil
	}
	endpoint := os.Getenv("HONEYCOMB_OTLP_ENDPOINT")
	if endpoint == "" {
		endpoint = "api.honeycomb.io"
	}
	exp, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpoint(endpoint), otlptracehttp.WithHeaders(map[string]string{"x-honeycomb-team": key}), otlptracehttp.WithURLPath("/v1/traces"))
	if err != nil {
		return err
	}
	res, _ := resource.New(ctx, resource.WithAttributes(semconv.ServiceName("genai-observability-evaluator")))
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp), sdktrace.WithResource(res))
	defer tp.Shutdown(ctx)
	tr := tp.Tracer("genai-observability/evaluation")
	_, span := tr.Start(ctx, "gen_ai.evaluation")
	span.SetAttributes(attribute.String("trace.reference", out.Request.TraceID), attribute.String("gen_ai.request.model", out.Request.Model), attribute.String("prompt.version", out.Request.PromptVersion), attribute.Float64("evaluation.mean", out.Consensus.MeanScore), attribute.Float64("evaluation.judge_agreement", out.Consensus.JudgeAgreement), attribute.Float64("evaluation.max_disagreement", out.Consensus.MaxDisagreement), attribute.Int("evaluation.judge_count", out.Consensus.JudgeCount))
	for i, r := range out.Results {
		span.SetAttributes(attribute.String(fmt.Sprintf("evaluation.%d.name", i), r.Name), attribute.Float64(fmt.Sprintf("evaluation.%d.value", i), r.Value), attribute.String(fmt.Sprintf("evaluation.%d.concept", i), r.Semantics.Concept), attribute.String(fmt.Sprintf("evaluation.%d.evaluator", i), r.Evaluator.Provider+":"+r.Evaluator.Model))
	}
	span.End()
	return tp.ForceFlush(ctx)
}
func main() { lambda.Start(handler) }
