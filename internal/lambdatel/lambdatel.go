package lambdatel

import (
	"context"
	"log/slog"
	"os"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/grace/genai-observability/internal/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

var (
	once sync.Once
	tp   *sdktrace.TracerProvider
)

// Setup is called from every handler. It deliberately never fails a request:
// telemetry trouble must not fail a Step Functions execution and re-run the
// expensive judges. It must, however, be loud - the previous version returned an
// error that every caller discarded, so a tracer provider that was never built
// looked exactly like a healthy one.

func Setup(ctx context.Context, service string) error {
	var setupErr error
	once.Do(func() {
		// Export failures are asynchronous and otherwise invisible. Without this
		// handler there is no way to tell "spans failed to send" from "spans were
		// never created".
		otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
			slog.Error("otel error", "service", service, "err", err)
		}))

		key := os.Getenv("HONEYCOMB_API_KEY")
		if key == "" {
			arn := os.Getenv("HONEYCOMB_API_KEY_SECRET_ARN")
			if arn != "" {
				cfg, err := config.LoadDefaultConfig(ctx)
				if err != nil {
					setupErr = err
					return
				}
				sec, err := secretsmanager.NewFromConfig(cfg).GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: aws.String(arn)})
				if err != nil {
					setupErr = err
					return
				}
				if sec.SecretString != nil {
					key = *sec.SecretString
				}
			}
		}
		endpoint := os.Getenv("HONEYCOMB_OTLP_ENDPOINT")
		if endpoint == "" {
			endpoint = "api.honeycomb.io"
		}
		// Never log the key itself, only whether one was resolved.
		slog.Info("telemetry setup",
			"service", service,
			"endpoint", endpoint,
			"api_key_present", key != "",
			"secret_arn_configured", os.Getenv("HONEYCOMB_API_KEY_SECRET_ARN") != "")
		opts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(endpoint), otlptracehttp.WithURLPath("/v1/traces")}
		if key != "" {
			opts = append(opts, otlptracehttp.WithHeaders(map[string]string{"x-honeycomb-team": key}))
		}
		exp, err := otlptracehttp.New(ctx, opts...)
		if err != nil {
			setupErr = err
			return
		}
		res, err := resource.New(ctx, resource.WithAttributes(semconv.ServiceName(service)))
		if err != nil {
			setupErr = err
			return
		}
		tp = sdktrace.NewTracerProvider(sdktrace.WithBatcher(exp), sdktrace.WithResource(res))
		otel.SetTracerProvider(tp)
		otel.SetTextMapPropagator(propagation.TraceContext{})
	})
	if setupErr != nil {
		slog.Error("telemetry setup failed; continuing without tracing", "service", service, "err", setupErr)
	}
	return setupErr
}

func Start(ctx context.Context, carrier map[string]string, name string) (context.Context, trace.Span) {
	ctx = telemetry.ExtractTraceContext(ctx, carrier)
	return otel.Tracer("genai-observability/lambda").Start(ctx, name)
}

// Flush exports buffered spans before the Lambda environment freezes.
//
// A nil provider means Setup never completed. That is reported rather than
// returning nil, which previously made a broken setup indistinguishable from a
// run that simply had nothing to send.
func Flush(ctx context.Context) error {
	if tp == nil {
		slog.Warn("flush skipped: no tracer provider (setup did not complete)")
		return nil
	}
	if err := tp.ForceFlush(ctx); err != nil {
		slog.Error("span flush failed; spans for this invocation were lost", "err", err)
		return err
	}
	return nil
}
