package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	ebtypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/grace/genai-observability/internal/lambdatel"
	"github.com/grace/genai-observability/internal/model"
	"github.com/grace/genai-observability/internal/pricing"
	"github.com/grace/genai-observability/internal/replay"
	"github.com/grace/genai-observability/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
)

type Input struct {
	Bucket        string `json:"bucket"`
	Key           string `json:"key"`
	Model         string `json:"model,omitempty"`
	PromptVersion string `json:"prompt_version,omitempty"`
}

type ReplayComparison struct {
	OriginalTraceID  string   `json:"original_trace_id"`
	ReplayTraceID    string   `json:"replay_trace_id"`
	Model            string   `json:"model"`
	PromptVersion    string   `json:"prompt_version"`
	InputTokens      int64    `json:"input_tokens"`
	OutputTokens     int64    `json:"output_tokens"`
	LatencyMS        float64  `json:"latency_ms"`
	CostUSD          *float64 `json:"cost_usd,omitempty"`
	CostDeltaUSD     *float64 `json:"cost_delta_usd,omitempty"`
	LatencyDeltaMS   float64  `json:"latency_delta_ms"`
	PricingVersion   string   `json:"pricing_version,omitempty"`
	EvaluationQueued bool     `json:"evaluation_queued"`
}

var s3c *s3.Client
var br *bedrockruntime.Client
var eb *eventbridge.Client

func init() {
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		panic(err)
	}
	s3c = s3.NewFromConfig(cfg)
	br = bedrockruntime.NewFromConfig(cfg)
	eb = eventbridge.NewFromConfig(cfg)
}

func handler(ctx context.Context, in Input) (ReplayComparison, error) {
	obj, err := s3c.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(in.Bucket), Key: aws.String(in.Key)})
	if err != nil {
		return ReplayComparison{}, err
	}
	defer obj.Body.Close()
	raw, err := io.ReadAll(obj.Body)
	if err != nil {
		return ReplayComparison{}, err
	}
	var stored model.HarnessOutput
	if err := json.Unmarshal(raw, &stored); err != nil {
		return ReplayComparison{}, err
	}

	if err := lambdatel.Setup(ctx, "genai-observability-replay-worker"); err != nil {
		slog.Error("telemetry unavailable for this invocation", "err", err)
	}
	ctx, span := lambdatel.Start(ctx, stored.Request.TraceContext, "replay.execute")
	defer func() {
		span.End()
		if err := lambdatel.Flush(ctx); err != nil {
			slog.Error("span flush failed", "err", err)
		}
	}()

	modelID := in.Model
	if modelID == "" {
		modelID = os.Getenv("REPLAY_MODEL_ID")
	}
	if modelID == "" {
		modelID = stored.Request.Model
	}
	pv := in.PromptVersion
	if pv == "" {
		pv = "replay-v1"
	}
	prompt := fmt.Sprintf("Counterfactual replay %s. Answer using only the evidence.\nQuestion: %s\nEvidence:\n- %s", pv, stored.Request.Question, strings.Join(stored.Request.Evidence, "\n- "))
	started := time.Now()
	resp, err := br.Converse(ctx, &bedrockruntime.ConverseInput{
		ModelId:         aws.String(modelID),
		Messages:        []brtypes.Message{{Role: brtypes.ConversationRoleUser, Content: []brtypes.ContentBlock{&brtypes.ContentBlockMemberText{Value: prompt}}}},
		InferenceConfig: &brtypes.InferenceConfiguration{MaxTokens: aws.Int32(500), Temperature: aws.Float32(0.2)},
	})
	if err != nil {
		return ReplayComparison{}, err
	}
	latencyMS := float64(time.Since(started).Microseconds()) / 1000.0
	answer := ""
	if msg, ok := resp.Output.(*brtypes.ConverseOutputMemberMessage); ok {
		for _, b := range msg.Value.Content {
			if t, ok := b.(*brtypes.ContentBlockMemberText); ok {
				answer += t.Value
			}
		}
	}
	var inTok, outTok int64
	if resp.Usage != nil {
		if resp.Usage.InputTokens != nil {
			inTok = int64(*resp.Usage.InputTokens)
		}
		if resp.Usage.OutputTokens != nil {
			outTok = int64(*resp.Usage.OutputTokens)
		}
	}

	req := stored.Request
	req.TraceID = fmt.Sprintf("replay-%d-%s", time.Now().UnixNano(), stored.Request.TraceID)
	req.SpanID = ""
	req.Answer = answer
	req.Model = modelID
	req.PromptVersion = pv
	req.InputTokens = inTok
	req.OutputTokens = outTok
	req.LatencyMS = latencyMS
	req.CostUSD = nil
	req.PricingVersion = ""
	req.ObservedAt = time.Now().UTC()
	if req.Attributes == nil {
		req.Attributes = map[string]string{}
	}
	req.Attributes["replay.of_trace_id"] = stored.Request.TraceID
	req.TraceContext = telemetry.InjectTraceContext(ctx)

	comparison := ReplayComparison{OriginalTraceID: stored.Request.TraceID, ReplayTraceID: req.TraceID, Model: modelID, PromptVersion: pv, InputTokens: inTok, OutputTokens: outTok, LatencyMS: latencyMS}
	if c, err := pricingCatalog(); err == nil {
		if cost, err := pricing.Calculate(c, modelID, inTok, outTok); err == nil {
			req.CostUSD = &cost.USD
			req.PricingVersion = cost.PricingVersion
			comparison.CostUSD = &cost.USD
			comparison.PricingVersion = cost.PricingVersion
		}
	}
	delta := replay.Compare(
		replay.Baseline{LatencyMS: stored.Request.LatencyMS, CostUSD: stored.Request.CostUSD},
		replay.Candidate{LatencyMS: comparison.LatencyMS, CostUSD: comparison.CostUSD},
	)
	comparison.LatencyDeltaMS = delta.LatencyMS
	comparison.CostDeltaUSD = delta.CostUSD
	span.SetAttributes(
		attribute.String("replay.of_trace_id", stored.Request.TraceID),
		attribute.String("gen_ai.request.model", modelID),
		attribute.Int64("gen_ai.usage.input_tokens", inTok),
		attribute.Int64("gen_ai.usage.output_tokens", outTok),
		attribute.Float64("gen_ai.response.latency_ms", latencyMS),
		attribute.Float64("replay.latency_delta_ms", comparison.LatencyDeltaMS),
	)
	if comparison.CostUSD != nil {
		span.SetAttributes(attribute.Float64("gen_ai.usage.cost_usd", *comparison.CostUSD))
	}
	if comparison.CostDeltaUSD != nil {
		span.SetAttributes(attribute.Float64("replay.cost_delta_usd", *comparison.CostDeltaUSD))
	}

	b, _ := json.Marshal(req)
	out, err := eb.PutEvents(ctx, &eventbridge.PutEventsInput{Entries: []ebtypes.PutEventsRequestEntry{{EventBusName: aws.String(os.Getenv("EVENT_BUS_NAME")), Source: aws.String("genai.observability.replay"), DetailType: aws.String("EvaluationRequested"), Detail: aws.String(string(b))}}})
	if err != nil {
		return ReplayComparison{}, err
	}
	if out.FailedEntryCount > 0 {
		return ReplayComparison{}, fmt.Errorf("event publish failed")
	}
	comparison.EvaluationQueued = true
	return comparison, nil
}

func pricingCatalog() (pricing.Catalog, error) {
	if raw := os.Getenv("MODEL_PRICING_JSON"); raw != "" {
		return pricing.ParseCatalogJSON(raw)
	}
	return pricing.DefaultCatalog()
}

func main() { lambda.Start(handler) }
