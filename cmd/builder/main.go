// Command builder runs the instrumented adapter-builder agent against a sample
// telemetry payload from a framework this repository does not yet support.
//
// It emits OpenTelemetry GenAI spans (invoke_agent, chat, execute_tool) so the
// run can be inspected as an agent trace. Point OTEL_EXPORTER_OTLP_ENDPOINT and
// OTEL_EXPORTER_OTLP_HEADERS at a collector or vendor to see it.
//
//	go run ./cmd/builder -system langfuse -payload ./payload.json
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/grace/genai-observability/internal/builder"
	"github.com/grace/genai-observability/internal/pricing"
	"github.com/grace/genai-observability/internal/telemetry"
)

func main() {
	var (
		system    = flag.String("system", "", "name of the source framework, e.g. langfuse")
		payload   = flag.String("payload", "", "path to a sample telemetry payload (JSON)")
		outDir    = flag.String("out", "examples/normalization", "directory to write the fixture and mapping proposal into")
		modelID   = flag.String("model", os.Getenv("BUILDER_MODEL_ID"), "Bedrock model or inference profile id (or set BUILDER_MODEL_ID)")
		maxIter   = flag.Int("max-iterations", 3, "maximum refine iterations")
		runTests  = flag.Bool("run-tests", false, "run go test ./... as a final verification step")
		dryRunLLM = flag.Bool("dry-run", false, "skip the model call and propose nothing; useful to verify the span shape")
	)
	flag.Parse()

	if *system == "" || *payload == "" {
		fmt.Fprintln(os.Stderr, "both -system and -payload are required")
		flag.Usage()
		os.Exit(2)
	}
	if *modelID == "" && !*dryRunLLM {
		fmt.Fprintln(os.Stderr, "-model (or BUILDER_MODEL_ID) is required unless -dry-run is set")
		os.Exit(2)
	}

	ctx := context.Background()
	shutdown, err := telemetry.Setup(ctx, "genai-observability-builder")
	if err != nil {
		log.Fatalf("telemetry setup: %v", err)
	}
	defer func() {
		if err := shutdown(context.Background()); err != nil {
			log.Printf("telemetry shutdown: %v", err)
		}
	}()

	var client builder.ModelClient
	if *dryRunLLM {
		client = noopModel{}
	} else {
		cfg, err := config.LoadDefaultConfig(ctx)
		if err != nil {
			log.Fatalf("aws config: %v", err)
		}
		client = &bedrockModel{br: bedrockruntime.NewFromConfig(cfg), modelID: *modelID}
	}

	catalog, err := pricing.DefaultCatalog()
	if err != nil {
		// Cost attribution is a nice-to-have; a missing catalog must not stop a run.
		log.Printf("pricing catalog unavailable, continuing without cost attribution: %v", err)
	}

	res, err := builder.Run(ctx, client, builder.Options{
		System:        *system,
		PayloadPath:   *payload,
		OutDir:        *outDir,
		MaxIterations: *maxIter,
		RunTests:      *runTests,
		Catalog:       catalog,
	})
	if err != nil {
		log.Fatalf("builder: %v", err)
	}

	out, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		log.Fatalf("encode result: %v", err)
	}
	fmt.Println(string(out))

	// A proposal needing review is the expected conservative outcome, not a
	// failure, so it must not be reported as one.
	if res.RequiresHumanReview {
		fmt.Fprintf(os.Stderr, "\n%d of %d field(s) need human review before this adapter is written.\n",
			len(res.UnknownFields), len(res.Proposals))
	}
	if res.TestsRan && !res.TestsPassed {
		os.Exit(1)
	}
}

// bedrockModel adapts Bedrock Converse to the agent's ModelClient interface.
type bedrockModel struct {
	br      *bedrockruntime.Client
	modelID string
}

func (b *bedrockModel) Chat(ctx context.Context, prompt string) (builder.Reply, error) {
	out, err := b.br.Converse(ctx, &bedrockruntime.ConverseInput{
		ModelId: aws.String(b.modelID),
		Messages: []brtypes.Message{{
			Role:    brtypes.ConversationRoleUser,
			Content: []brtypes.ContentBlock{&brtypes.ContentBlockMemberText{Value: prompt}},
		}},
		InferenceConfig: &brtypes.InferenceConfiguration{
			MaxTokens:   aws.Int32(1500),
			Temperature: aws.Float32(0),
		},
	})
	if err != nil {
		return builder.Reply{Model: b.modelID}, err
	}

	reply := builder.Reply{Model: b.modelID}
	if msg, ok := out.Output.(*brtypes.ConverseOutputMemberMessage); ok {
		var sb strings.Builder
		for _, block := range msg.Value.Content {
			if t, ok := block.(*brtypes.ContentBlockMemberText); ok {
				sb.WriteString(t.Value)
			}
		}
		reply.Text = sb.String()
	}
	if out.Usage != nil {
		if out.Usage.InputTokens != nil {
			reply.InputTokens = int64(*out.Usage.InputTokens)
		}
		if out.Usage.OutputTokens != nil {
			reply.OutputTokens = int64(*out.Usage.OutputTokens)
		}
	}
	return reply, nil
}

// noopModel proposes nothing, so every field resolves to UNKNOWN. It exists to
// exercise the agent's span shape without spending tokens.
type noopModel struct{}

func (noopModel) Chat(context.Context, string) (builder.Reply, error) {
	return builder.Reply{Model: "dry-run", Text: `{"proposals":[]}`}, nil
}
