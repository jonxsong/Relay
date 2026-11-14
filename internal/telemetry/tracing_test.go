package telemetry

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

func TestConfigureTracingInstallsW3CPropagationWithoutExporter(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	shutdown, err := ConfigureTracing(context.Background(), "relay-test", "development")
	if err != nil {
		t.Fatalf("ConfigureTracing() error = %v", err)
	}
	carrier := propagation.MapCarrier{}
	remote := propagation.TraceContext{}.Extract(context.Background(), propagation.MapCarrier{"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"})
	otel.GetTextMapPropagator().Inject(remote, carrier)
	if carrier.Get("traceparent") == "" {
		t.Fatal("configured propagator did not inject traceparent")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown() error = %v", err)
	}
}
