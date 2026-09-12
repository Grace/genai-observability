# Agent-assisted semantic mapping

The semantic mapping workflow is a control-plane aid for maintainers adding or upgrading framework adapters. It does **not** normalize production events and it does **not** edit adapter code.

## Goal

Given a source concept and a proposed OpenTelemetry/canonical target, produce a reviewable assessment of whether the concepts are semantically equivalent and what would be lost by mapping them.

Allowed relationships:

- `EXACT`
- `COMPATIBLE`
- `NARROWER`
- `BROADER`
- `LOSSY`
- `CONFLICTING`
- `UNKNOWN`

`UNKNOWN` is a successful outcome when evidence is insufficient.

## Workflow

```text
MappingRequest
    |
    v
+-------------------- parallel --------------------+
| semantic analyst | OTel analyst | loss analyst   |
+------------------------+--------------------------+
                         |
                         v
                adversarial reviewer
                         |
                         v
              deterministic aggregator
                         |
              +----------+----------+
              |                     |
      CANDIDATE_APPROVAL           REVIEW
              |                     |
              +----------+----------+
                         |
                  engineer decision
                         |
                         v
        versioned deterministic adapter mapping
```

The agents are intentionally specialized:

- **semantic**: compares the natural-language meaning of source and target concepts;
- **otel**: checks the proposed mapping against supplied OTel semantic-convention context;
- **loss**: looks for units, subsets, billing semantics, cardinality, provenance, or other information that would be lost;
- **reviewer**: tries to falsify the other agents' conclusion and identify unsupported assumptions.

The deterministic aggregation policy is deliberately conservative. `LOSSY`, `CONFLICTING`, `UNKNOWN`, objections, low confidence, or disagreement trigger human review. Only unanimous, high-confidence `EXACT`/`COMPATIBLE` analyses without objections can become a candidate for approval—and even that does not mutate production mappings.

## Why agents are not in the runtime normalization path

Runtime telemetry normalization must be reproducible, low-latency, testable, cost-bounded, and backward compatible. Accepted mappings therefore become deterministic versioned adapter logic plus fixtures/conformance tests.

Agent analysis is useful where deterministic code is weak: comparing prose definitions, spotting concept drift, and challenging plausible-but-wrong equivalence.

## Example

```bash
./scripts/mapping-analysis.sh \
  "$(terraform -chdir=infra output -raw mapping_state_machine_arn)" \
  examples/mapping/genkit-input-tokens.json
```

Use `examples/mapping/ambiguous-quality-score.json` to exercise an intentionally under-specified mapping that should be escalated rather than silently treated as equivalent.
