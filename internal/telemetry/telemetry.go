package telemetry

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"google.golang.org/grpc/credentials/insecure"
)

// Setup configures tracing and returns a shutdown function.
//
// Exporter configuration is taken from the standard OTLP environment variables
// so the same binary can talk to a local collector or to a TLS vendor endpoint
// without a code change:
//
//	OTEL_EXPORTER_OTLP_PROTOCOL  "grpc" (default) or "http/protobuf"
//	OTEL_EXPORTER_OTLP_ENDPOINT  e.g. https://api.honeycomb.io, or localhost:4317
//	OTEL_EXPORTER_OTLP_HEADERS   e.g. x-honeycomb-team=<ingest key>
//
// TLS is decided by the endpoint: an https:// scheme, or any non-loopback host,
// uses TLS. Only http:// or a loopback address falls back to plaintext, which is
// what a local collector expects.
func Setup(ctx context.Context, service string) (func(context.Context) error, error) {
	// OTEL_TRACES_EXPORTER=console writes spans to stdout instead of shipping
	// them. It makes span shape inspectable without a collector or a vendor
	// account, which is how the agent traces are checked locally.
	if strings.EqualFold(os.Getenv("OTEL_TRACES_EXPORTER"), "console") {
		return setupConsole(ctx, service)
	}
	exp, err := newExporter(ctx)
	if err != nil {
		return nil, err
	}
	res, err := resource.New(ctx, resource.WithAttributes(
		semconv.ServiceName(service),
		semconv.ServiceVersion("1.0.0"),
	))
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp, sdktrace.WithBatchTimeout(2*time.Second)),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	return tp.Shutdown, nil
}

func newExporter(ctx context.Context) (*otlptrace.Exporter, error) {
	raw := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	host, secure := endpointTarget(raw)

	switch protocol() {
	case "http/protobuf", "http":
		opts := []otlptracehttp.Option{}
		if host != "" {
			opts = append(opts, otlptracehttp.WithEndpoint(host))
		}
		if !secure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		return otlptracehttp.New(ctx, opts...)
	case "grpc":
		opts := []otlptracegrpc.Option{}
		if host != "" {
			opts = append(opts, otlptracegrpc.WithEndpoint(host))
		}
		if !secure {
			opts = append(opts, otlptracegrpc.WithTLSCredentials(insecure.NewCredentials()))
		}
		return otlptracegrpc.New(ctx, opts...)
	default:
		return nil, fmt.Errorf("telemetry: unsupported OTEL_EXPORTER_OTLP_PROTOCOL %q (want grpc or http/protobuf)", protocol())
	}
}

func protocol() string {
	p := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL"))
	if p == "" {
		return "grpc"
	}
	return strings.ToLower(p)
}

// endpointTarget splits an OTLP endpoint into the host:port form the exporters
// expect and whether the connection should use TLS.
//
// The scheme decides when present. Without one, only loopback is treated as
// plaintext: defaulting a remote host to insecure would fail against every
// hosted endpoint, which is the failure this function exists to prevent.
func endpointTarget(raw string) (host string, secure bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		// No endpoint configured: the SDK default is a local collector.
		return "", false
	}
	switch {
	case strings.HasPrefix(raw, "https://"):
		return strings.TrimSuffix(strings.TrimPrefix(raw, "https://"), "/"), true
	case strings.HasPrefix(raw, "http://"):
		return strings.TrimSuffix(strings.TrimPrefix(raw, "http://"), "/"), false
	}
	raw = strings.TrimSuffix(raw, "/")
	return raw, !isLoopback(raw)
}

func isLoopback(hostPort string) bool {
	host := hostPort
	if i := strings.LastIndex(host, ":"); i >= 0 && !strings.Contains(host[i+1:], "]") {
		host = host[:i]
	}
	host = strings.Trim(host, "[]")
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || host == ""
}

func setupConsole(ctx context.Context, service string) (func(context.Context) error, error) {
	exp, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
	if err != nil {
		return nil, err
	}
	res, err := resource.New(ctx, resource.WithAttributes(
		semconv.ServiceName(service),
		semconv.ServiceVersion("1.0.0"),
	))
	if err != nil {
		return nil, err
	}
	// Synchronous export: a short-lived CLI must not lose spans to batching.
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp), sdktrace.WithResource(res))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	return tp.Shutdown, nil
}
