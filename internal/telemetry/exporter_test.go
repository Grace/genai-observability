package telemetry

import (
	"context"
	"testing"
)

func TestEndpointTarget(t *testing.T) {
	cases := []struct {
		name       string
		raw        string
		wantHost   string
		wantSecure bool
	}{
		// The case that previously could not work at all: a hosted HTTPS
		// endpoint was forced onto plaintext credentials.
		{"honeycomb https", "https://api.honeycomb.io", "api.honeycomb.io", true},
		{"honeycomb https trailing slash", "https://api.honeycomb.io/", "api.honeycomb.io", true},
		{"eu region", "https://api.eu1.honeycomb.io", "api.eu1.honeycomb.io", true},
		{"explicit http stays plaintext", "http://collector:4318", "collector:4318", false},
		{"bare loopback is plaintext", "localhost:4317", "localhost:4317", false},
		{"loopback ip is plaintext", "127.0.0.1:4317", "127.0.0.1:4317", false},
		// A bare remote host must not silently downgrade to plaintext.
		{"bare remote host uses tls", "otlp.example.com:4317", "otlp.example.com:4317", true},
		{"empty means sdk default", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			host, secure := endpointTarget(tc.raw)
			if host != tc.wantHost {
				t.Errorf("host = %q, want %q", host, tc.wantHost)
			}
			if secure != tc.wantSecure {
				t.Errorf("secure = %v, want %v", secure, tc.wantSecure)
			}
		})
	}
}

func TestProtocolDefaultsToGRPC(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "")
	if got := protocol(); got != "grpc" {
		t.Errorf("protocol() = %q, want grpc", got)
	}
}

func TestProtocolIsCaseInsensitive(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "HTTP/Protobuf")
	if got := protocol(); got != "http/protobuf" {
		t.Errorf("protocol() = %q, want http/protobuf", got)
	}
}

func TestIsLoopback(t *testing.T) {
	for _, s := range []string{"localhost", "localhost:4317", "127.0.0.1:4317", "[::1]:4317"} {
		if !isLoopback(s) {
			t.Errorf("isLoopback(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"api.honeycomb.io", "api.honeycomb.io:443", "otlp.example.com:4317"} {
		if isLoopback(s) {
			t.Errorf("isLoopback(%q) = true, want false", s)
		}
	}
}

func TestUnsupportedProtocolIsAnError(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "carrier-pigeon")
	if _, err := newExporter(context.Background()); err == nil {
		t.Fatal("expected an error for an unsupported protocol")
	}
}
