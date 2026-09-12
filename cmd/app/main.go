package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	ebtypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	"github.com/grace/genai-observability/internal/canonical"
	"github.com/grace/genai-observability/internal/model"
	"github.com/grace/genai-observability/internal/normalize"
	"github.com/grace/genai-observability/internal/pricing"
	"github.com/grace/genai-observability/internal/telemetry"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type app struct {
	eb      *eventbridge.Client
	br      *bedrockruntime.Client
	bus     string
	modelID string
}

type askRequest struct {
	Question string   `json:"question"`
	Evidence []string `json:"evidence"`
}

func main() {
	ctx := context.Background()
	shutdown, err := telemetry.Setup(ctx, "genai-observability-app")
	if err != nil {
		log.Fatal(err)
	}
	defer shutdown(ctx)
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatal(err)
	}
	a := &app{
		eb: eventbridge.NewFromConfig(cfg), br: bedrockruntime.NewFromConfig(cfg),
		bus: os.Getenv("EVENT_BUS_NAME"), modelID: os.Getenv("CANDIDATE_MODEL_ID"),
	}
	if a.modelID == "" {
		log.Fatal("CANDIDATE_MODEL_ID is required")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/ask", a.ask)
	mux.HandleFunc("/normalize", a.normalize)
	log.Printf("listening on :8080; candidate model=%s", a.modelID)
	handler := cors(mux)
	log.Fatal(http.ListenAndServe(":8080", otelhttp.NewHandler(handler, "http.server")))
}

func cors(next http.Handler) http.Handler {
	origin := os.Getenv("ALLOWED_ORIGIN")
	if origin == "" {
		origin = "*"
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Traceparent, Tracestate")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *app) normalize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var req normalize.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	report, err := normalize.Run(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_, span := otel.Tracer("genai-observability/normalizer").Start(r.Context(), "gen_ai.telemetry.normalize")
	span.SetAttributes(
		attribute.String("gen_ai.operation.name", report.Record.Operation),
		attribute.String("gen_ai.provider.name", report.Record.Provider),
		attribute.String("gen_ai.request.model", report.Record.Model),
		attribute.Int64("gen_ai.usage.input_tokens", report.Record.InputTokens),
		attribute.Int64("gen_ai.usage.output_tokens", report.Record.OutputTokens),
		attribute.Int64("gen_ai.usage.total_tokens", report.Record.TotalTokens),
		attribute.String("telemetry.source", report.Record.Source.System),
		attribute.Int("normalization.warning_count", len(report.Warnings)),
		attribute.Int("normalization.error_count", len(report.Errors)),
	)
	// The counts alone cannot answer "which warning started spiking, for which
	// source, on which field" - the question this service exists to answer. Emit
	// the codes and fields as dimensions so they can be grouped and differentiated.
	span.SetAttributes(issueAttributes("warning", report.Warnings)...)
	span.SetAttributes(issueAttributes("error", report.Errors)...)
	span.End()
	w.Header().Set("content-type", "application/json")
	if len(report.Errors) > 0 {
		w.WriteHeader(http.StatusUnprocessableEntity)
	}
	_ = json.NewEncoder(w).Encode(report)
}

func (a *app) ask(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body askRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	ctx, span := otel.Tracer("genai-observability/app").Start(ctx, "gen_ai.request")
	defer span.End()
	span.SetAttributes(attribute.String("gen_ai.request.model", a.modelID), attribute.String("gen_ai.operation.name", "chat"))

	started := time.Now()
	answer, inTok, outTok, err := a.generate(ctx, body)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		http.Error(w, err.Error(), 502)
		return
	}
	latencyMS := float64(time.Since(started).Microseconds()) / 1000.0
	span.SetAttributes(attribute.Int("gen_ai.usage.input_tokens", inTok), attribute.Int("gen_ai.usage.output_tokens", outTok), attribute.Float64("gen_ai.response.latency_ms", latencyMS))

	sc := trace.SpanContextFromContext(ctx)
	traceID, spanID := sc.TraceID().String(), sc.SpanID().String()
	if !sc.IsValid() {
		traceID = fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	req := model.EvalRequest{
		TraceID: traceID, SpanID: spanID, Question: body.Question, Answer: answer, Evidence: body.Evidence,
		Model: a.modelID, PromptVersion: "candidate-v1", ObservedAt: time.Now().UTC(),
		InputTokens: int64(inTok), OutputTokens: int64(outTok), LatencyMS: latencyMS,
		TraceContext: telemetry.InjectTraceContext(ctx),
		Attributes:   map[string]string{"source": "ecs", "service": "genai-observability-app"},
	}
	if c, err := pricingCatalog(); err == nil {
		if cost, err := pricing.Calculate(c, a.modelID, int64(inTok), int64(outTok)); err == nil {
			req.CostUSD = &cost.USD
			req.PricingVersion = cost.PricingVersion
			span.SetAttributes(attribute.Float64("gen_ai.usage.cost_usd", cost.USD), attribute.String("gen_ai.usage.pricing_version", cost.PricingVersion))
		}
	}
	queued := a.publishEvaluation(ctx, req) == nil
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"trace_id": traceID, "answer": answer, "evaluation_queued": queued})
}

func (a *app) generate(ctx context.Context, body askRequest) (string, int, int, error) {
	prompt := fmt.Sprintf("Answer the question using only the evidence. If evidence is insufficient, say so.\nQuestion: %s\nEvidence:\n- %s", body.Question, strings.Join(body.Evidence, "\n- "))
	out, err := a.br.Converse(ctx, &bedrockruntime.ConverseInput{
		ModelId:         aws.String(a.modelID),
		Messages:        []brtypes.Message{{Role: brtypes.ConversationRoleUser, Content: []brtypes.ContentBlock{&brtypes.ContentBlockMemberText{Value: prompt}}}},
		InferenceConfig: &brtypes.InferenceConfiguration{MaxTokens: aws.Int32(500), Temperature: aws.Float32(0.2)},
	})
	if err != nil {
		return "", 0, 0, err
	}
	var answer string
	if msg, ok := out.Output.(*brtypes.ConverseOutputMemberMessage); ok {
		for _, b := range msg.Value.Content {
			if t, ok := b.(*brtypes.ContentBlockMemberText); ok {
				answer += t.Value
			}
		}
	}
	inTok, outTok := 0, 0
	if out.Usage != nil {
		if out.Usage.InputTokens != nil {
			inTok = int(*out.Usage.InputTokens)
		}
		if out.Usage.OutputTokens != nil {
			outTok = int(*out.Usage.OutputTokens)
		}
	}
	return answer, inTok, outTok, nil
}

func (a *app) publishEvaluation(ctx context.Context, req model.EvalRequest) error {
	b, _ := json.Marshal(req)
	out, err := a.eb.PutEvents(ctx, &eventbridge.PutEventsInput{Entries: []ebtypes.PutEventsRequestEntry{{
		EventBusName: aws.String(a.bus), Source: aws.String("genai.observability.app"), DetailType: aws.String("EvaluationRequested"), Detail: aws.String(string(b)),
	}}})
	if err != nil {
		return err
	}
	if out.FailedEntryCount > 0 {
		return fmt.Errorf("EventBridge rejected %d event(s)", out.FailedEntryCount)
	}
	return nil
}

func pricingCatalog() (pricing.Catalog, error) {
	if raw := os.Getenv("MODEL_PRICING_JSON"); raw != "" {
		return pricing.ParseCatalogJSON(raw)
	}
	return pricing.DefaultCatalog()
}

// issueAttributes turns normalization issues into span dimensions. Codes and
// fields are sorted and deduplicated so the attribute value is stable for a
// given set of issues regardless of detection order.
func issueAttributes(kind string, issues []canonical.Issue) []attribute.KeyValue {
	if len(issues) == 0 {
		return nil
	}
	codes := make([]string, 0, len(issues))
	fields := make([]string, 0, len(issues))
	seenCode := map[string]bool{}
	seenField := map[string]bool{}
	for _, is := range issues {
		if is.Code != "" && !seenCode[is.Code] {
			seenCode[is.Code] = true
			codes = append(codes, is.Code)
		}
		if is.Field != "" && !seenField[is.Field] {
			seenField[is.Field] = true
			fields = append(fields, is.Field)
		}
	}
	sort.Strings(codes)
	sort.Strings(fields)
	out := make([]attribute.KeyValue, 0, 2)
	if len(codes) > 0 {
		out = append(out, attribute.StringSlice("normalization."+kind+".codes", codes))
	}
	if len(fields) > 0 {
		out = append(out, attribute.StringSlice("normalization."+kind+".fields", fields))
	}
	return out
}
