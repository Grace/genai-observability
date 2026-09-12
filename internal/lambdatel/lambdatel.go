package lambdatel

import (
	"context"
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

func Setup(ctx context.Context, service string) error {
	var setupErr error
	once.Do(func() {
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
	return setupErr
}

func Start(ctx context.Context, carrier map[string]string, name string) (context.Context, trace.Span) {
	ctx = telemetry.ExtractTraceContext(ctx, carrier)
	return otel.Tracer("genai-observability/lambda").Start(ctx, name)
}

func Flush(ctx context.Context) error {
	if tp == nil {
		return nil
	}
	return tp.ForceFlush(ctx)
}
