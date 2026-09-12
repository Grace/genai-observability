# Architecture

## 1. Telemetry normalization plane

The normalization plane is the foundation of the system. Source adapters convert known provider/framework shapes into a versioned GenAI telemetry model. Validation then distinguishes mappings that are safe from observations that are ambiguous or internally inconsistent.

```mermaid
flowchart LR
    O[OTel GenAI] --> A[Source adapters]
    L[OpenLLMetry] --> A
    B[Braintrust] --> A
    BR[Bedrock] --> A
    A --> M[GenAI telemetry model v1]
    M --> N[Validation + normalization]
    N --> R[Normalization report]
    N --> T[OTel-aligned telemetry]
    T --> H[Honeycomb]
```

The deployed ECS service exposes this as `POST /normalize`. Normalized attributes and warning/error counts are emitted as an OpenTelemetry span through the ADOT sidecar.

## 2. Production request plane

```mermaid
flowchart LR
    U[Client] --> ALB[Application Load Balancer]
    ALB --> APP[Go AI App / ECS Fargate]
    APP --> BR[Amazon Bedrock / Candidate Model]
    APP --> OTEL[ADOT Collector / Sidecar]
    OTEL --> HC[Honeycomb]
    APP --> EB[EventBridge / EvaluationRequested]
```

The user-facing request does not wait for evaluation. It calls the candidate model, records production telemetry, publishes an evaluation event, and returns.

## 3. Evaluation plane

```mermaid
flowchart TD
    EB[EventBridge] --> SFN[Step Functions Evaluation Harness]
    SFN --> S[Schema Evaluator]
    SFN --> P[Policy Evaluator]
    SFN --> OA[Oracle A]
    SFN --> OB[Oracle B]
    OA --> BA[Bedrock]
    OB --> BB[Bedrock]
    S --> C[Consensus]
    P --> C
    OA --> C
    OB --> C
    C --> N[Evaluation Normalizer]
    N --> S3[S3 Corpus]
    N --> DDB[DynamoDB State]
    N --> HC[Honeycomb]
```

The two oracle results remain separate. Consensus computes aggregate statistics and disagreement; it does not assert that the ensemble is ground truth.

## 4. Evaluation representation

```json
{
  "name": "groundedness",
  "kind": "probability",
  "value": 0.91,
  "confidence": 0.86,
  "evaluator": {
    "type": "llm_judge",
    "provider": "aws.bedrock",
    "model": "...",
    "version": "oracle-a"
  },
  "semantics": {
    "concept": "groundedness",
    "scale_min": 0,
    "scale_max": 1,
    "higher_is_better": true,
    "unit": "probability"
  }
}
```

The telemetry normalizer follows the same rule: a numerical value is not made comparable across sources unless the mapping establishes compatible semantics.

## 5. Persistence

```text
evaluations/<trace-id>.json         # normalized evaluation output
corpus/production/<trace-id>.json   # replay input corpus
replay-runs/...                     # Distributed Map result writer output
```

DynamoDB stores operational evaluation state. Honeycomb remains the analytical observability surface.

## 6. Counterfactual replay

```mermaid
flowchart LR
    S3[S3 production corpus] --> DM[Step Functions Distributed Map]
    DM --> RW[Replay Worker Lambda]
    RW --> ALT[Alternate Bedrock model/prompt]
    ALT --> EB[EvaluationRequested]
    EB --> EH[Same Evaluation Harness]
```

Replay outputs carry `replay.of_trace_id` and are deliberately excluded from `corpus/production/`, preventing replay-generated cases from recursively expanding the replay corpus.

## 7. Failure philosophy

The system prefers an explicit warning/error over a silent semantic guess. Examples include unknown evaluation-score semantics, incompatible token accounting, missing required canonical fields, and version-specific schema drift.
