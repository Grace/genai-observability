package telemetry

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

func TestTraceContextRoundTrip(t *testing.T) {
	otel.SetTextMapPropagator(propagation.TraceContext{})
	tid, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	sid, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
	sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: tid, SpanID: sid, TraceFlags: trace.FlagsSampled})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)
	carrier := InjectTraceContext(ctx)
	got := trace.SpanContextFromContext(ExtractTraceContext(context.Background(), carrier))
	if got.TraceID() != tid || got.SpanID() != sid {
		t.Fatalf("trace context did not round-trip: %v", got)
	}
}
