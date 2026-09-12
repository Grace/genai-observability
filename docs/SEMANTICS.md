# Normalization and semantic safety

## Canonical model vs. semantic conventions

The canonical GenAI telemetry model is an internal integration model. OpenTelemetry semantic conventions remain the external interoperability target. The internal model exists so source-specific adapters can be tested independently from export behavior and so ambiguous mappings have an explicit place to be represented.

## Mapping outcomes

A source mapping should be treated as one of three outcomes:

- **exact** — source and target definitions are known to represent the same concept and compatible units/types;
- **compatible** — a documented transformation preserves the intended interpretation;
- **unknown** — there is not enough information to establish equivalence.

The reference code currently exposes the last category through warnings rather than inventing a conversion.

## Scores

A scalar score is not automatically a probability. Before cross-system comparison, a production implementation should establish at least:

- semantic concept (e.g. groundedness, relevance, toxicity);
- value kind (boolean, scalar, probability, category, vector);
- valid domain/scale;
- direction (`higher_is_better` where applicable);
- evaluator identity and version;
- optional confidence and evidence/provenance.

## Token usage

Provider/framework token fields may use different names and accounting rules. Known aliases can map into `input_tokens`, `output_tokens`, and `total_tokens`. If a supplied total disagrees with input + output, the reference normalizer preserves the reported total and emits `usage_mismatch` rather than rewriting the observation.

## Unknown fields

The model carries source payload data in `extensions` in this reference implementation. A production service could make this more selective for privacy/cost reasons, but silent dropping during adapter development makes schema drift harder to diagnose.

## Schema evolution

The canonical record contains `schema_version`. Version-specific source adapters/fixtures should be added whenever an upstream change alters meaning, not merely spelling. Breaking semantic changes should not be hidden behind alias tables.
