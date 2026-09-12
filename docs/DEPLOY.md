# Deployment and Verification

## Preconditions

Check tooling:

```bash
aws sts get-caller-identity
terraform version
docker version
go version
```

Ensure the chosen Bedrock model IDs/inference profiles are usable in the configured AWS region.

## Honeycomb secret

```bash
export AWS_REGION=us-east-1
export HONEYCOMB_API_KEY='your-key'
ARN=$(./scripts/create-honeycomb-secret.sh)
echo "$ARN"
```

The secret value is not placed in `terraform.tfvars`.

## Terraform values

```bash
cp infra/terraform.tfvars.example infra/terraform.tfvars
```

At minimum set:

```hcl
candidate_model_id = "..."
oracle_model_id_a = "..."
oracle_model_id_b = "..."
replay_model_id = "..."
mapping_model_id = "..."
honeycomb_api_key_secret_arn = "arn:aws:secretsmanager:..."
```

Set `model_pricing_json` to a versioned catalog containing rates verified for the exact candidate/replay model IDs if you want dollar-cost fields. Unknown models intentionally produce no fabricated cost.

Using the same model for both oracle slots is valid for an infrastructure smoke test, but using independent models gives disagreement a more meaningful interpretation.

## Full deploy

```bash
./scripts/deploy.sh
```

Expected Terraform outputs:

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

## Verify serving

```bash
ALB=$(terraform -chdir=infra output -raw alb_url)
curl "$ALB/healthz"
```

Expected: `ok`.

## Verify end-to-end evaluation

```bash
./scripts/smoke-test.sh
```

Then inspect:

1. ECS service: task healthy and registered in the ALB target group.
2. CloudWatch logs: `/ecs/genai-observability/app` and `/ecs/genai-observability/otel`.
3. EventBridge custom bus: matching `EvaluationRequested` events.
4. Step Functions: `genai-observability-evaluation` shows four parallel evaluator branches, consensus, and normalization.
5. S3: `evaluations/<trace-id>.json` and `corpus/production/<trace-id>.json` exist.
6. DynamoDB: `TRACE#<trace-id>` has an `EVAL#...` record.
7. Honeycomb: the ECS app plus instrumented evaluator/replay Lambda service names appear, with asynchronous spans sharing propagated trace context.

## Verify replay

```bash
./scripts/replay.sh '<alternate-model-id>' replay-v2
```

Inspect the `genai-observability-replay` state machine. Each corpus item invokes the alternate model and publishes a fresh evaluation event. New evaluation records include `replay.of_trace_id` in the request attributes. Replay outputs are excluded from `corpus/production/`. After replay evaluation completes, inspect `replay-comparisons/<replay-trace-id>.json` for quality, cost, and latency deltas.

## Destroy

```bash
make destroy
```

The corpus bucket is configured with `force_destroy=true` for development convenience. Remove that behavior before relying on the bucket as production evidence storage.

## Troubleshooting

### ECS cannot pull app image
Run the deployment script rather than a first-time plain `terraform apply`; it creates ECR and pushes the app before creating the service.

### Bedrock AccessDenied / ValidationException
Confirm the exact model or inference profile is available in the selected region and the account is authorized to invoke it.

### Collector fails to start
Inspect `/ecs/<project>/otel`. The ADOT task uses `--config=env:AOT_CONFIG_CONTENT`, and the Honeycomb key is injected from Secrets Manager.

### App works but no evaluation appears
Check the response's `evaluation_queued` field, then inspect the custom EventBridge bus and Step Functions execution history.

### Honeycomb has production traces but not evaluation traces
Check the normalizer Lambda's Secrets Manager permission and Lambda logs. Evaluation persistence is intentionally independent from Honeycomb export.
