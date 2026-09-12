// Package semconv holds the set of OpenTelemetry GenAI attribute names this
// repository recognises as real.
//
// It exists so a proposed mapping target can be checked against the convention
// instead of being trusted. A model asked to map telemetry will readily invent
// plausible-looking attributes - observed in practice: llm.model.name,
// llm.usage.prompt_tokens, trace.id, trace.duration. Each is wrong in a way that
// is invisible downstream: the data lands, the column name looks reasonable, and
// nothing joins with anyone else's telemetry.
//
// Snapshot of the gen_ai namespace at semantic conventions v1.41.0. Refresh it
// by re-reading the registry (the Honeycomb MCP server's semconv tool, or the
// upstream semantic-conventions repository) rather than by hand.
package semconv

import "strings"

// Version is the semantic conventions release this snapshot was taken from.
const Version = "1.41.0"

// genAIAttributes is the gen_ai namespace: span/attribute-group names, event
// attributes, and metric names.
var genAIAttributes = map[string]bool{
	"gen_ai.agent.description":                      true,
	"gen_ai.agent.id":                               true,
	"gen_ai.agent.name":                             true,
	"gen_ai.agent.version":                          true,
	"gen_ai.client.operation.duration":              true,
	"gen_ai.client.operation.time_per_output_chunk": true,
	"gen_ai.client.operation.time_to_first_chunk":   true,
	"gen_ai.client.token.usage":                     true,
	"gen_ai.conversation.id":                        true,
	"gen_ai.data_source.id":                         true,
	"gen_ai.embeddings.dimension.count":             true,
	"gen_ai.evaluation.explanation":                 true,
	"gen_ai.evaluation.name":                        true,
	"gen_ai.evaluation.score.label":                 true,
	"gen_ai.evaluation.score.value":                 true,
	"gen_ai.input.messages":                         true,
	"gen_ai.operation.name":                         true,
	"gen_ai.output.messages":                        true,
	"gen_ai.output.type":                            true,
	"gen_ai.prompt.name":                            true,
	"gen_ai.provider.name":                          true,
	"gen_ai.request.choice.count":                   true,
	"gen_ai.request.encoding_formats":               true,
	"gen_ai.request.frequency_penalty":              true,
	"gen_ai.request.max_tokens":                     true,
	"gen_ai.request.model":                          true,
	"gen_ai.request.presence_penalty":               true,
	"gen_ai.request.seed":                           true,
	"gen_ai.request.stop_sequences":                 true,
	"gen_ai.request.stream":                         true,
	"gen_ai.request.temperature":                    true,
	"gen_ai.request.top_k":                          true,
	"gen_ai.request.top_p":                          true,
	"gen_ai.response.finish_reasons":                true,
	"gen_ai.response.id":                            true,
	"gen_ai.response.model":                         true,
	"gen_ai.response.time_to_first_chunk":           true,
	"gen_ai.retrieval.documents":                    true,
	"gen_ai.server.request.duration":                true,
	"gen_ai.server.time_per_output_token":           true,
	"gen_ai.server.time_to_first_token":             true,
	"gen_ai.system_instructions":                    true,
	"gen_ai.token.type":                             true,
	"gen_ai.tool.definitions":                       true,
	"gen_ai.tool.name":                              true,
	"gen_ai.tool.type":                              true,
	"gen_ai.usage.cache_creation.input_tokens":      true,
	"gen_ai.usage.cache_read.input_tokens":          true,
	"gen_ai.usage.input_tokens":                     true,
	"gen_ai.usage.output_tokens":                    true,
	"gen_ai.usage.reasoning.output_tokens":          true,
}

// legacyAttributes were valid in earlier drafts and still appear in the wild.
// They are recognised so they can be reported as legacy rather than as unknown,
// but they are not valid mapping targets: normalizing onto a superseded name
// reintroduces the fragmentation this repository exists to remove.
var legacyAttributes = map[string]bool{
	"gen_ai.system":                  true,
	"gen_ai.usage.prompt_tokens":     true,
	"gen_ai.usage.completion_tokens": true,
}

// Known reports whether name is a current gen_ai attribute.
func Known(name string) bool {
	return genAIAttributes[strings.TrimSpace(name)]
}

// Legacy reports whether name is a superseded gen_ai attribute.
func Legacy(name string) bool {
	return legacyAttributes[strings.TrimSpace(name)]
}

// InNamespace reports whether name is in the gen_ai namespace at all, current or
// not. A target outside it is not necessarily wrong - a repository may carry its
// own attributes - but it is outside what this convention can vouch for.
func InNamespace(name string) bool {
	return strings.HasPrefix(strings.TrimSpace(name), "gen_ai.")
}

// Count returns how many current attributes are registered, for reporting.
func Count() int { return len(genAIAttributes) }
