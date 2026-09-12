package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/grace/genai-observability/internal/lambdatel"
	"github.com/grace/genai-observability/internal/mapping"
	"go.opentelemetry.io/otel/attribute"
)

var br *bedrockruntime.Client
var object = regexp.MustCompile(`(?s)\{.*\}`)

type agentJSON struct {
	Relationship mapping.Relationship `json:"relationship"`
	Confidence   float64              `json:"confidence"`
	Rationale    string               `json:"rationale"`
	Evidence     []mapping.Evidence   `json:"evidence"`
	Objections   []string             `json:"objections"`
}

func init() {
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		panic(err)
	}
	br = bedrockruntime.NewFromConfig(cfg)
}

func handler(ctx context.Context, input json.RawMessage) (mapping.AgentAssessment, error) {
	role := os.Getenv("MAPPING_AGENT_ROLE")
	modelID := os.Getenv("BEDROCK_MODEL_ID")
	if role == "" || modelID == "" {
		return mapping.AgentAssessment{}, fmt.Errorf("MAPPING_AGENT_ROLE and BEDROCK_MODEL_ID are required")
	}
	carrier := traceContextFromInput(input)
	_ = lambdatel.Setup(ctx, "genai-observability-mapping-"+role)
	ctx, span := lambdatel.Start(ctx, carrier, "mapping."+role)
	defer func() { span.End(); _ = lambdatel.Flush(ctx) }()
	span.SetAttributes(attribute.String("mapping.agent.role", role), attribute.String("gen_ai.request.model", modelID))
	prompt := rolePrompt(role) + `\nReturn ONLY JSON with relationship (EXACT|COMPATIBLE|NARROWER|BROADER|LOSSY|CONFLICTING|UNKNOWN), confidence 0..1, rationale, evidence[{kind,source,claim}], objections[]. Never invent documentation. UNKNOWN is preferred to unsupported equivalence.\nInput:\n` + string(input)
	out, err := br.Converse(ctx, &bedrockruntime.ConverseInput{ModelId: aws.String(modelID), Messages: []types.Message{{Role: types.ConversationRoleUser, Content: []types.ContentBlock{&types.ContentBlockMemberText{Value: prompt}}}}, InferenceConfig: &types.InferenceConfiguration{MaxTokens: aws.Int32(900), Temperature: aws.Float32(0)}})
	if err != nil {
		return mapping.AgentAssessment{}, err
	}
	text := ""
	if msg, ok := out.Output.(*types.ConverseOutputMemberMessage); ok {
		for _, b := range msg.Value.Content {
			if t, ok := b.(*types.ContentBlockMemberText); ok {
				text += t.Value
			}
		}
	}
	candidate := text
	if m := object.FindString(text); m != "" {
		candidate = m
	}
	var parsed agentJSON
	if err := json.Unmarshal([]byte(candidate), &parsed); err != nil {
		return mapping.AgentAssessment{Agent: role, Relationship: mapping.Unknown, Confidence: 0, Rationale: "agent returned invalid structured output", Objections: []string{"unparseable model response"}, Model: modelID, PromptVersion: "mapping-agent-v1"}, nil
	}
	if !validRelationship(parsed.Relationship) {
		parsed.Relationship = mapping.Unknown
		parsed.Confidence = 0
		parsed.Objections = append(parsed.Objections, "invalid relationship classification")
	}
	if parsed.Confidence < 0 {
		parsed.Confidence = 0
	}
	if parsed.Confidence > 1 {
		parsed.Confidence = 1
	}
	span.SetAttributes(attribute.String("mapping.relationship", string(parsed.Relationship)), attribute.Float64("mapping.confidence", parsed.Confidence), attribute.Int("mapping.objection_count", len(parsed.Objections)))
	return mapping.AgentAssessment{Agent: role, Relationship: parsed.Relationship, Confidence: parsed.Confidence, Rationale: parsed.Rationale, Evidence: parsed.Evidence, Objections: parsed.Objections, Model: modelID, PromptVersion: "mapping-agent-v1"}, nil
}

func traceContextFromInput(input json.RawMessage) map[string]string {
	var direct struct {
		TraceContext map[string]string `json:"trace_context"`
	}
	if json.Unmarshal(input, &direct) == nil && len(direct.TraceContext) > 0 {
		return direct.TraceContext
	}
	var wrapped struct {
		Request struct {
			TraceContext map[string]string `json:"trace_context"`
		} `json:"request"`
	}
	if json.Unmarshal(input, &wrapped) == nil {
		return wrapped.Request.TraceContext
	}
	return nil
}

func rolePrompt(role string) string {
	switch role {
	case "semantic":
		return "You are the semantic-equivalence analyst. Compare the natural-language meaning of the source and target concepts. Focus on whether they denote the same observable concept."
	case "otel":
		return "You are the OpenTelemetry semantic-conventions analyst. Evaluate whether the proposed target preserves the source meaning and whether OTel terminology/constraints support the mapping. Do not claim spec facts absent from the provided context."
	case "loss":
		return "You are the information-loss analyst. Try to find source semantics, units, subsets, billing rules, cardinality, or provenance that would be lost or distorted by the target mapping."
	case "reviewer":
		return "You are the adversarial reviewer. Inspect the mapping request and other agents' assessments. Try to falsify their conclusion, identify unsupported assumptions, correlated reasoning, or missing evidence."
	default:
		return "You are a conservative telemetry mapping analyst."
	}
}
func validRelationship(r mapping.Relationship) bool {
	return strings.Contains("|EXACT|COMPATIBLE|NARROWER|BROADER|LOSSY|CONFLICTING|UNKNOWN|", "|"+string(r)+"|")
}
func main() { lambda.Start(handler) }
