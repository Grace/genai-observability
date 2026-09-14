# GenAI Observability

A Go reference system for normalizing heterogeneous GenAI telemetry into an OpenTelemetry-aligned model, evaluating production behavior, replaying real traces against alternate models/prompts, and using agent-assisted semantic analysis to review new framework mappings without putting nondeterministic decisions in the runtime normalization path.

The project is intentionally built around ordinary infrastructure terminology: adapters, telemetry model, normalization, semantic conventions, evaluation, replay, trace propagation, pricing, and review policy.

## Live

**https://genai-observability.wirewitch.ai** — the Normalization Inspector, running against this
repository's deployed AWS stack. Paste a framework payload, see the canonical record and every
warning the normalizer raised.

Try this one. Two aliases disagree about the input token count, and the source reports a total that
contradicts its own components:

```json
{"source":"otel","version":"demo","payload":{"attributes":{
  "gen_ai.operation.name":"chat",
  "gen_ai.usage.input_tokens":10,
  "gen_ai.usage.prompt_tokens":9999,
  "gen_ai.usage.output_tokens":4,
  "gen_ai.usage.total_tokens":0
}}}
```

It returns `alias_conflict` and `usage_mismatch`, keeps the first alias by documented precedence,
and preserves the reported zero rather than quietly replacing it with the sum. Reporting that
disagreement instead of resolving it silently is the point of the project.

No sign-in is needed: `/normalize` invokes no model and costs nothing per request. `/ask`, which does
call a model, lives on a separate hostname behind a Cognito login, because an unauthenticated
endpoint that spends tokens is a way to spend someone else's money.

Nothing to install:

```bash
go run ./cmd/normalize -file examples/normalization/braintrust.json
```

## What it demonstrates

1. Framework/provider telemetry adapters for OpenTelemetry GenAI, OpenLLMetry/Traceloop, Braintrust, Bedrock, Pydantic AI, Eino, and Genkit.
2. A versioned canonical GenAI telemetry model that preserves provenance and reports ambiguity instead of inventing semantic equivalence.
3. OpenTelemetry instrumentation across the HTTP path and asynchronous evaluation/replay boundaries using W3C trace context carried in event payloads.
4. An AWS evaluation harness with deterministic schema/policy checks, two Bedrock LLM judges, disagreement/consensus telemetry, S3 persistence, and DynamoDB state.
5. Counterfactual replay against an alternate Bedrock model with token usage, latency, versioned token pricing, and post-evaluation quality/cost/latency deltas.
6. An advisory agentic semantic-mapping workflow: semantic, OTel, and information-loss analysts plus an adversarial reviewer feed a deterministic policy that returns `CANDIDATE_APPROVAL` or `REVIEW`.
7. A React + TypeScript Normalization Inspector that calls the same `/normalize` API and renders the canonical record plus warnings/errors.
8. Terraform for the complete AWS reference deployment and CI checks for Go, Terraform, scripts, and the web client.

## Architecture

```text
                        HETEROGENEOUS TELEMETRY

 OTel GenAI   OpenLLMetry   Braintrust   Bedrock   Pydantic AI   Eino   Genkit
      \            |            |           |           |          |      /
       +-----------+------------+-----------+-----------+----------+-----+
                                      |
                                      v
                               source adapters
                                      |
                                      v
                       canonical GenAI telemetry model
                                      |
                         validation / normalization
                                      |
                    +-----------------+------------------+
                    |                                    |
                    v                                    v
               OTel telemetry                     normalization report
                    |                                    |
                    v                                    v
                Honeycomb                    React/TypeScript Inspector


                         PRODUCTION / EVALUATION PLANE

client -> ALB -> ECS Go service -> Bedrock candidate model
                  |       |
                  |       +--> ADOT -> Honeycomb
                  |
                  +--> EventBridge EvaluationRequested
                           (W3C trace context carried in payload)
                                   |
                                   v
                           Step Functions Standard
                    +---------+---------+---------+---------+
                    |         |         |         |         |
                  schema    policy   oracle A  oracle B
                    |         |         |         |
                    +---------+---------+---------+
                                   |
                               consensus
                                   |
                                   v
                        evaluation normalizer
                          /        |        \
                        S3       DynamoDB   Honeycomb
                         |
                         +--> corpus/production/
                                   |
                                   v
                       Distributed Map replay
                                   |
                          alternate Bedrock model
                                   |
                    usage + latency + token pricing
                                   |
                          same evaluation harness
                                   |
                                   v
                    replay-comparisons/<trace>.json
                      quality / cost / latency deltas


                       SEMANTIC-MAPPING CONTROL PLANE

source concept + candidate OTel target
                  |
        +---------+---------+
        |         |         |
    semantic     OTel      loss
      agent      agent     agent
        +---------+---------+
                  |
          adversarial reviewer
                  |
                  v
        deterministic aggregation
           /                \
 CANDIDATE_APPROVAL        REVIEW
           \                /
             engineer decision
                    |
                    v
          deterministic adapter change
```

The agentic mapping workflow is advisory. It does not edit runtime adapters, mutate the canonical model, or decide telemetry semantics on the ingestion hot path.

## Repository map

```text
cmd/app/                    ECS Go service: /ask, /normalize, /healthz
cmd/normalize/              standalone normalization CLI
internal/adapters/          source-specific telemetry adapters
internal/canonical/         canonical telemetry model + normalization reports
internal/normalize/         normalization + semantic-safety validation
internal/model/             evaluation and replay persistence models
internal/pricing/           versioned token-price catalog + cost calculation
internal/replay/            baseline/candidate comparison logic
internal/telemetry/         OTel setup + W3C context injection/extraction
internal/lambdatel/         Lambda OTLP exporter + remote-parent restoration
internal/mapping/           mapping contracts + deterministic review policy
lambda/                     evaluation, replay, mapping agents, aggregation
infra/                      full AWS Terraform deployment
web/                        React + TypeScript Normalization Inspector
otel/                       ADOT/reference collector configuration
examples/                   normalization, evaluation, mapping fixtures
scripts/                    build, deploy, smoke, replay, mapping helpers
docs/                       architecture, semantics, mapping, deployment, validation,
                            Claude Code self-observability
```

## Normalization: preserve meaning before convenience

The normalizer intentionally refuses to infer semantics from field names or numeric values alone. For example, a Braintrust score of `0.91` is retained as a scalar unless the source establishes scale, direction, and evaluator meaning; it is not silently promoted to a probability.

A reported token total that disagrees with `input + output` is preserved and returned with `usage_mismatch` rather than rewritten. The source value remains inspectable in `extensions`.

```json
{
  "record": {
    "operation": "chat",
    "input_tokens": 10,
    "output_tokens": 4,
    "total_tokens": 20
  },
  "warnings": [
    {
      "code": "usage_mismatch",
      "field": "total_tokens",
      "message": "reported total tokens differ from input + output; preserved without rewriting"
    }
  ]
}
```

## Framework adapters

Adapters translate source-specific shapes into the same telemetry concepts; they do not call models themselves.

| Source | Example source concept | Canonical concept |
|---|---|---|
| OTel/Pydantic AI | `gen_ai.usage.input_tokens` | input tokens |
| OpenLLMetry | `llm.usage.prompt_tokens` | input tokens, with alias warning |
| Eino | `ChatModel` | `chat` operation |
| Genkit | `span_type=generate` | `chat` operation |
| Bedrock | `usage.inputTokens` | input tokens |
| Braintrust | evaluator score | scalar evaluation unless semantics are explicit |

`internal/normalize/normalize_test.go` includes a cross-framework conformance test showing equivalent Pydantic AI, Eino, and Genkit model calls normalize to the same operation/provider/model/token totals.

## Trace continuity

`EvalRequest.TraceContext` carries W3C `traceparent`/`tracestate` values across EventBridge and Step Functions payloads. Lambda entry points reconstruct the remote parent before starting evaluation spans. Replay starts from the production trace context, creates a replay span, and injects the replay context into the next asynchronous evaluation request.

This keeps causal observability explicit across boundaries instead of relying only on a string trace reference. `internal/telemetry/context_test.go` exercises context round-tripping.

Verified in the deployed system. A single request to `/ask` produced one trace of eight spans across
six services, every evaluation span carrying the producing request's span as its parent rather than
correlating on a string attribute:

```text
http.server                    genai-observability-app          (root)
└── gen_ai.request             genai-observability-app
    └── [EventBridge -> Step Functions]
        ├── evaluation.schema      genai-observability-schema-eval
        ├── evaluation.policy      genai-observability-policy-eval
        ├── evaluation.oracle x2   genai-observability-oracle-eval
        ├── evaluation.consensus   genai-observability-consensus
        └── evaluation.persist     genai-observability-normalizer
```

Crossing that boundary is the part that usually breaks. An evaluation that cannot be traced back to
the request that caused it is a number without a provenance, and a trace that stops at the queue
cannot answer why one customer's requests are slow.

## Evaluation harness

The Step Functions evaluation workflow runs four branches:

- deterministic schema validity;
- deterministic demo credential-leak policy;
- Bedrock LLM judge A;
- Bedrock LLM judge B.

The consensus step preserves the two judges as separate measurements and calculates mean, standard deviation, maximum disagreement, agreement, and judge count. LLM agreement is telemetry, not ground truth.

The evaluation normalizer persists results to S3/DynamoDB and emits evaluation attributes through OTLP to Honeycomb.

## Cost accounting and replay

Token cost is calculated by `internal/pricing` from a versioned catalog:

```text
cost = input_tokens / 1,000,000 * input_rate
     + output_tokens / 1,000,000 * output_rate
```

Unknown models fail closed: no dollar value is invented. `MODEL_PRICING_JSON` lets a deployment supply rates verified for its actual model IDs and region without changing code.

Replay reads only `corpus/production/`, invokes the alternate model, records candidate token usage/latency/cost, then sends the result through the same evaluation harness. When replay evaluation completes, `lambda/normalizer` loads the baseline evaluation and writes:

```text
replay-comparisons/<replay-trace-id>.json
```

with:

```text
quality_baseline / quality_replay / quality_delta
cost_baseline_usd / cost_replay_usd / cost_delta_usd
latency_baseline_ms / latency_replay_ms / latency_delta_ms
```

Replay outputs are not added back to `corpus/production/`, preventing replay feedback loops.

## Agent-assisted semantic mapping

The semantic-mapping workflow is for adapter maintainers confronting a new framework field or version change. Agents classify a candidate relation as:

`EXACT`, `COMPATIBLE`, `NARROWER`, `BROADER`, `LOSSY`, `CONFLICTING`, or `UNKNOWN`.

Each agent returns confidence, evidence, rationale, and objections. The adversarial reviewer is explicitly asked to falsify proposed equivalence. `internal/mapping/policy.go` then applies deterministic policy: uncertainty, disagreement, lossy/conflicting mappings, objections, or low confidence require human review. A high-confidence exact/compatible result can only become `CANDIDATE_APPROVAL`; it still does not modify runtime mappings.

See `docs/SEMANTIC_MAPPING.md` and `examples/mapping/`.

## Normalization Inspector

The `web/` client is a small Vite + React + TypeScript interface. It accepts a normalization payload, calls `POST /normalize`, renders the canonical record, and lists semantic warnings/errors separately.

```bash
cd web
npm install
VITE_NORMALIZE_API=http://localhost:8080 npm run dev
```

## Local normalization

```bash
go run ./cmd/normalize -file examples/normalization/otel.json
go run ./cmd/normalize -file examples/normalization/openllmetry.json
go run ./cmd/normalize -file examples/normalization/braintrust.json
go run ./cmd/normalize -file examples/normalization/bedrock.json
go run ./cmd/normalize -file examples/normalization/pydanticai.json
go run ./cmd/normalize -file examples/normalization/eino.json
go run ./cmd/normalize -file examples/normalization/genkit.json
```

## Tests and validation

Run before deployment:

```bash
go mod tidy
git diff --exit-code -- go.mod go.sum
go test ./...
go vet ./...
terraform -chdir=infra fmt -check -recursive
terraform -chdir=infra init -backend=false
terraform -chdir=infra validate
bash -n scripts/*.sh
(cd web && npm install && npm run build)
```

CI runs the same classes of checks on push and pull request. See `docs/VALIDATION.md`.

## AWS deployment

Prerequisites: AWS CLI, Terraform >= 1.7, Docker, Go 1.23+, zip, Bedrock model access, and a Honeycomb API key.

```bash
export AWS_REGION=us-east-1
export HONEYCOMB_API_KEY='...'
./scripts/create-honeycomb-secret.sh

cp infra/terraform.tfvars.example infra/terraform.tfvars
# Configure model IDs, secret ARN, and a verified MODEL_PRICING_JSON catalog.

./scripts/deploy.sh
./scripts/normalization-smoke-test.sh
./scripts/smoke-test.sh
```

Counterfactual replay:

```bash
./scripts/replay.sh '<alternate-bedrock-model-id>' replay-v2
```

Semantic mapping analysis:

```bash
./scripts/mapping-analysis.sh \
  "$(terraform -chdir=infra output -raw mapping_state_machine_arn)" \
  examples/mapping/genkit-input-tokens.json
```

## Terraform outputs

```text
alb_url
ecr_repository_url
event_bus_name
evaluation_state_machine_arn
replay_state_machine_arn
mapping_state_machine_arn
corpus_bucket
eval_table
```

## Self-observability

Claude Code exports its own OpenTelemetry metrics, log events, and spans. `docs/SELF_OBSERVABILITY.md` records the configuration, the Honeycomb dataset routing (metrics land in the shared `metrics` dataset; the `x-honeycomb-dataset` header is ignored), the places the vendor documentation does not match the wire format, and the privacy consequences of enabling content capture. `docs/claude-code-board.json` holds the validated query specs for the **Claude Code Monitoring** board, and `scripts/claude-code-telemetry.env.example` holds the exporter configuration.

## Security and operating boundaries

- Honeycomb API key is referenced by Secrets Manager ARN; it is not stored in Terraform variables as plaintext secret material.
- Runtime LLM mapping decisions are deterministic; agents are advisory only.
- Unknown pricing or semantics fail closed rather than fabricating data.
- The demo policy evaluator is intentionally narrow and is not presented as a complete security classifier.
- Production content retention should be reviewed before enabling prompt/completion storage in a real environment.

## License

Apache License 2.0. See `LICENSE`.
