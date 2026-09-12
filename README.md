# GenAI Observability — Full AWS Reference System

A production-oriented reference implementation for **normalizing heterogeneous GenAI telemetry into an OpenTelemetry-aligned telemetry model**, evaluating production behavior, and replaying historical traces against alternative models or prompts.

The repository is intentionally named for what it does. There is no custom vocabulary required to understand the architecture: **adapters, telemetry model, normalization, semantic conventions, evaluation harness, and counterfactual replay**.

## Why this exists

GenAI instrumentation is fragmented. Providers, frameworks, and evaluation systems can describe the same workload using different attribute names, token accounting shapes, score types, and model/provider identifiers. Treating those values as equivalent just because they have similar names is unsafe.

This system demonstrates a practical approach:

1. ingest provider/framework-specific telemetry;
2. adapt it into a versioned GenAI telemetry model;
3. normalize known aliases while surfacing ambiguity and information loss;
4. emit OpenTelemetry-compatible observability signals;
5. asynchronously evaluate real production executions;
6. normalize evaluation signals as telemetry;
7. replay the production corpus against alternative models/prompts and compare quality, cost, and latency.

## Architecture

```text
                         HETEROGENEOUS INPUTS

       OTel GenAI        OpenLLMetry        Braintrust        Bedrock
           |                  |                 |                |
           +------------------+-----------------+----------------+
                                      |
                                      v
                              Source Adapters
                                      |
                                      v
                        GenAI Telemetry Model (v1)
                                      |
                          validation / normalization
                                      |
                     +----------------+----------------+
                     |                                 |
                     v                                 v
              OpenTelemetry                     Evaluation request
                     |                                 |
                     v                                 v
                Honeycomb                         EventBridge
                                                      |
                                                      v
                                             Step Functions harness
                                          /        /        |       \
                                     schema    policy   oracle A  oracle B
                                                          |         |
                                                       Bedrock   Bedrock
                                          \        \        |       /
                                                   consensus
                                                       |
                                                       v
                                             eval normalization
                                           /          |          \
                                          S3       DynamoDB     Honeycomb
                                           |
                                           v
                                  production replay corpus
                                           |
                                           v
                              Step Functions Distributed Map
                                           |
                                           v
                                  replay-worker Lambda
                                           |
                                  alternate Bedrock model
                                           |
                                           +--> same eval harness
```

The user-facing `/ask` path is independent from evaluation. Evaluation is asynchronous and cannot add latency to the response path.

## Repository map

```text
cmd/app/                  ECS Go service: /ask, /normalize, /healthz
internal/canonical/       versioned GenAI telemetry model
internal/adapters/        OTel, OpenLLMetry, Braintrust, Bedrock adapters
internal/normalize/       validation + normalization reports
internal/model/           evaluation/replay model
internal/telemetry/       OpenTelemetry setup
lambda/                   schema, policy, oracle, consensus, eval normalizer, replay
infra/                    Terraform for VPC/ECS/ALB/ECR/Lambda/SFN/S3/DDB/EventBridge
otel/                     local/reference collector config
examples/                 evaluation + normalization examples
scripts/                  build, deploy, smoke, replay, secret creation
docs/                     architecture, deployment, validation, semantics
```

## Normalization contract

The normalizer does **not** flatten every number into a generic score. It preserves source values and reports uncertainty where semantics are not established.

Example request:

```json
{
  "source": "braintrust",
  "version": "example",
  "payload": {
    "name": "answer",
    "model": "example-model",
    "scores": {"groundedness": 0.91}
  }
}
```

The score is retained as a `scalar`; it is **not** silently promoted to a probability because a value of `0.91` alone does not establish a probability interpretation, scale, or direction.

Normalization returns both the record and a report:

```json
{
  "record": {"...": "..."},
  "warnings": [
    {
      "code": "unknown_score_semantics",
      "field": "scores",
      "message": "..."
    }
  ]
}
```

This is deliberate: **UNKNOWN is safer than false equivalence.**

## Supported demo adapters

- `otel` / `opentelemetry` — OpenTelemetry GenAI-style attributes
- `openllmetry` / `traceloop` — common OpenLLMetry aliases mapped to the canonical model
- `braintrust` — trace/evaluation payload shape with score semantics preserved conservatively
- `bedrock` — Amazon Bedrock usage/model/latency shape

These are reference adapters, not a claim that every historical version of each upstream project is represented. Each adapter is intentionally small enough to extend with version-specific mappings and fixtures.

## Production AWS plane

- **ALB** → **ECS Fargate** Go service
- **Amazon Bedrock Converse** for the candidate model
- **ADOT Collector sidecar** for OTLP export to Honeycomb
- **EventBridge** for asynchronous `EvaluationRequested` events
- **Step Functions Standard** evaluation harness
- deterministic **schema** and **policy** evaluator Lambdas
- two independently configurable **LLM-as-a-Judge** Bedrock evaluators
- **consensus/disagreement** Lambda
- evaluation normalizer/exporter
- **S3** evaluation corpus + replay corpus
- **DynamoDB** operational state/idempotency surface
- **Step Functions Distributed Map** for counterfactual replay
- **Secrets Manager** for the Honeycomb API key
- **ECR**, **CloudWatch Logs**, IAM, VPC, subnets, security groups

## Evaluation telemetry

Each evaluation result records:

- name and semantic concept;
- value kind (`boolean`, `probability`, etc.);
- scale and direction where known;
- evaluator type/model/version;
- confidence and explanation where available;
- trace reference.

The two LLM judges remain separate measurements. Consensus records mean, standard deviation, maximum disagreement, and agreement; it does not turn a judge ensemble into ground truth.

## Counterfactual replay

Only objects under `corpus/production/` are replay inputs. Replayed executions receive a new trace ID plus `replay.of_trace_id`, and **replay outputs are not re-added to the production corpus**. This prevents feedback-loop expansion of the replay set.

## Prerequisites

- AWS CLI authenticated to the target account
- Terraform >= 1.7
- Docker
- Go 1.23+
- `zip`
- Bedrock model access in your region
- Honeycomb API key

Default AWS region: `us-east-1`.

## Local tests

```bash
go test ./internal/normalize ./internal/adapters ./internal/canonical
```

For the full repository:

```bash
go mod tidy
go test ./...
```

## Try normalization locally

The standalone normalizer CLI has no AWS dependency:

```bash
go run ./cmd/normalize -file examples/normalization/otel.json
go run ./cmd/normalize -file examples/normalization/openllmetry.json
go run ./cmd/normalize -file examples/normalization/braintrust.json
go run ./cmd/normalize -file examples/normalization/bedrock.json
```

The deployed ECS service exposes the same normalization path at `POST /normalize` and emits a normalization span through the ADOT sidecar to Honeycomb.

## Deploy the full AWS system

### 1. Create the Honeycomb secret

```bash
export AWS_REGION=us-east-1
export HONEYCOMB_API_KEY='...'
./scripts/create-honeycomb-secret.sh
```

Copy the returned ARN.

### 2. Configure Terraform

```bash
cp infra/terraform.tfvars.example infra/terraform.tfvars
```

Set:

```hcl
candidate_model_id = "..."
oracle_model_id_a  = "..."
oracle_model_id_b  = "..."
replay_model_id    = "..."
honeycomb_api_key_secret_arn = "arn:aws:secretsmanager:..."
```

### 3. Deploy

```bash
./scripts/deploy.sh
```

The script builds Lambda packages, bootstraps ECR, builds/pushes the ECS image, and applies the complete Terraform stack.

### 4. Verify normalization

```bash
./scripts/normalization-smoke-test.sh
```

### 5. Verify production → async evaluation

```bash
./scripts/smoke-test.sh
```

### 6. Run counterfactual replay

```bash
./scripts/replay.sh '<alternate-bedrock-model-id>' replay-v2
```

## Terraform outputs

```text
alb_url
ecr_repository_url
event_bus_name
evaluation_state_machine_arn
replay_state_machine_arn
corpus_bucket
eval_table
```

## Design principles

- **Prefer established terminology.** The internal shared representation is a telemetry model/canonical model, not a branded IR.
- **Normalize syntax only when semantics justify it.** Similar names and equal numbers do not prove semantic equivalence.
- **Preserve provenance.** Provider/framework/evaluator identity remains queryable.
- **Make loss observable.** Warnings and errors are part of normalization output.
- **Keep production latency independent from evaluation.** Evaluation is asynchronous.
- **Treat model judges as measurements, not truth.** Disagreement is a useful signal.
- **Replay real production cases without contaminating the production corpus.**
- **Use OpenTelemetry as the interoperability boundary.** The project complements semantic conventions rather than inventing a competing telemetry standard.

## Validation status

See [`docs/VALIDATION.md`](docs/VALIDATION.md). The generated artifact is formatted and statically checked where tooling is available; live AWS deployment still requires your AWS account, selected Bedrock model IDs, and Honeycomb secret.
