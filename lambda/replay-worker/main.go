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
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	ebtypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/grace/genai-observability/internal/model"
)

type Input struct {
	Bucket        string `json:"bucket"`
	Key           string `json:"key"`
	Model         string `json:"model,omitempty"`
	PromptVersion string `json:"prompt_version,omitempty"`
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

func handler(ctx context.Context, in Input) (map[string]any, error) {
	obj, err := s3c.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(in.Bucket), Key: aws.String(in.Key)})
	if err != nil {
		return nil, err
	}
	defer obj.Body.Close()
	raw, _ := io.ReadAll(obj.Body)
	var stored model.HarnessOutput
	if err := json.Unmarshal(raw, &stored); err != nil {
		return nil, err
	}
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
	resp, err := br.Converse(ctx, &bedrockruntime.ConverseInput{ModelId: aws.String(modelID), Messages: []brtypes.Message{{Role: brtypes.ConversationRoleUser, Content: []brtypes.ContentBlock{&brtypes.ContentBlockMemberText{Value: prompt}}}}, InferenceConfig: &brtypes.InferenceConfiguration{MaxTokens: aws.Int32(500), Temperature: aws.Float32(0.2)}})
	if err != nil {
		return nil, err
	}
	answer := ""
	if msg, ok := resp.Output.(*brtypes.ConverseOutputMemberMessage); ok {
		for _, b := range msg.Value.Content {
			if t, ok := b.(*brtypes.ContentBlockMemberText); ok {
				answer += t.Value
			}
		}
	}
	req := stored.Request
	req.TraceID = fmt.Sprintf("replay-%d-%s", time.Now().UnixNano(), stored.Request.TraceID)
	req.Answer = answer
	req.Model = modelID
	req.PromptVersion = pv
	if req.Attributes == nil {
		req.Attributes = map[string]string{}
	}
	req.Attributes["replay.of_trace_id"] = stored.Request.TraceID
	b, _ := json.Marshal(req)
	bus := os.Getenv("EVENT_BUS_NAME")
	out, err := eb.PutEvents(ctx, &eventbridge.PutEventsInput{Entries: []ebtypes.PutEventsRequestEntry{{EventBusName: aws.String(bus), Source: aws.String("genai.observability.replay"), DetailType: aws.String("EvaluationRequested"), Detail: aws.String(string(b))}}})
	if err != nil {
		return nil, err
	}
	if out.FailedEntryCount > 0 {
		return nil, fmt.Errorf("event publish failed")
	}
	return map[string]any{"original_trace_id": stored.Request.TraceID, "replay_trace_id": req.TraceID, "model": modelID, "prompt_version": pv}, nil
}
func main() { lambda.Start(handler) }
