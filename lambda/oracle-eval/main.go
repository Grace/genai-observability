package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/grace/genai-observability/internal/model"
)

type oracleJSON struct {
	Score      float64 `json:"score"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
}

var br *bedrockruntime.Client
var jsonObject = regexp.MustCompile(`(?s)\{.*\}`)

func init() {
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		panic(err)
	}
	br = bedrockruntime.NewFromConfig(cfg)
}

func handler(ctx context.Context, r model.EvalRequest) (model.EvalResult, error) {
	modelID := os.Getenv("BEDROCK_MODEL_ID")
	judge := os.Getenv("JUDGE_NAME")
	if judge == "" {
		judge = "oracle"
	}
	if modelID == "" {
		return model.EvalResult{}, fmt.Errorf("BEDROCK_MODEL_ID is required")
	}
	prompt := fmt.Sprintf(`You are %s, an independent production-quality evaluator. Judge ONLY how grounded the candidate answer is in the supplied evidence. Do not reward eloquence. Return ONLY JSON matching {"score":0.0,"confidence":0.0,"reason":"short reason"}. score and confidence are in [0,1].\nQuestion: %s\nEvidence: %v\nCandidate answer: %s`, judge, r.Question, r.Evidence, r.Answer)
	out, err := br.Converse(ctx, &bedrockruntime.ConverseInput{ModelId: aws.String(modelID), Messages: []types.Message{{Role: types.ConversationRoleUser, Content: []types.ContentBlock{&types.ContentBlockMemberText{Value: prompt}}}}, InferenceConfig: &types.InferenceConfiguration{MaxTokens: aws.Int32(300), Temperature: aws.Float32(0)}})
	if err != nil {
		return model.EvalResult{}, err
	}
	text := ""
	if msg, ok := out.Output.(*types.ConverseOutputMemberMessage); ok {
		for _, b := range msg.Value.Content {
			if t, ok := b.(*types.ContentBlockMemberText); ok {
				text += t.Value
			}
		}
	}
	var parsed oracleJSON
	candidate := text
	if m := jsonObject.FindString(text); m != "" {
		candidate = m
	}
	if err := json.Unmarshal([]byte(candidate), &parsed); err != nil {
		parsed = oracleJSON{Score: 0, Confidence: 0, Reason: "oracle returned non-JSON: " + strconv.Quote(text)}
	}
	if parsed.Score < 0 {
		parsed.Score = 0
	}
	if parsed.Score > 1 {
		parsed.Score = 1
	}
	if parsed.Confidence < 0 {
		parsed.Confidence = 0
	}
	if parsed.Confidence > 1 {
		parsed.Confidence = 1
	}
	return model.EvalResult{Name: "groundedness", Kind: "probability", Value: parsed.Score, Confidence: &parsed.Confidence, Reason: parsed.Reason, Evaluator: model.Evaluator{Type: "llm_judge", Provider: "aws.bedrock", Model: modelID, Version: judge}, Semantics: model.EvalSemantics{Concept: "groundedness", ScaleMin: 0, ScaleMax: 1, HigherIsBetter: true, Unit: "probability"}, TraceID: r.TraceID}, nil
}
func main() { lambda.Start(handler) }
