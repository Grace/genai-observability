package semconv

import "testing"

func TestKnownAcceptsCurrentAttributes(t *testing.T) {
	for _, a := range []string{
		"gen_ai.usage.input_tokens", "gen_ai.usage.output_tokens",
		"gen_ai.request.model", "gen_ai.response.model",
		"gen_ai.provider.name", "gen_ai.operation.name",
		"gen_ai.agent.name", "gen_ai.tool.name", "gen_ai.conversation.id",
		"gen_ai.usage.cache_read.input_tokens",
	} {
		if !Known(a) {
			t.Errorf("Known(%q) = false, want true", a)
		}
	}
}

// These are the names models actually invent, plus the superseded namespace.
func TestKnownRejectsInventedAndLegacy(t *testing.T) {
	for _, a := range []string{
		"llm.model.name", "llm.usage.prompt_tokens",
		"trace.id", "trace.duration", "opentelemetry.genai.latency_ms",
		"gen_ai.trace.id", "gen_ai.request.latency_ms",
		// Not defined by semconv despite being widely assumed.
		"gen_ai.usage.total_tokens",
		// Superseded: recognised as legacy, never a valid target.
		"gen_ai.system", "gen_ai.usage.prompt_tokens", "gen_ai.usage.completion_tokens",
	} {
		if Known(a) {
			t.Errorf("Known(%q) = true, want false", a)
		}
	}
}

func TestLegacyIsRecognisedSeparately(t *testing.T) {
	if !Legacy("gen_ai.usage.prompt_tokens") {
		t.Error("expected gen_ai.usage.prompt_tokens to be recognised as legacy")
	}
	if Legacy("gen_ai.usage.input_tokens") {
		t.Error("a current attribute must not be reported as legacy")
	}
}

func TestInNamespace(t *testing.T) {
	if !InNamespace("gen_ai.anything.at.all") {
		t.Error("expected gen_ai. prefix to be in namespace")
	}
	if InNamespace("llm.model.name") {
		t.Error("llm.* is not the gen_ai namespace")
	}
}

func TestExtensionsAreNotValidMappingTargets(t *testing.T) {
	// An extension is something this repository chooses to emit. A model
	// proposing one as a mapping target is still inventing an attribute, so
	// Known must keep excluding them.
	for _, a := range []string{"gen_ai.usage.cost_usd", "gen_ai.usage.pricing_version", "gen_ai.response.latency_ms"} {
		if !Extension(a) {
			t.Errorf("Extension(%q) = false, want true", a)
		}
		if Known(a) {
			t.Errorf("Known(%q) = true; an extension must not be a valid mapping target", a)
		}
		if ExtensionReason(a) == "" {
			t.Errorf("extension %q has no stated reason", a)
		}
	}
}
