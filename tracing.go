package tracing

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
)

var serviceName string

// InitTracer configures the global Otel TracerProvider with an OTLP HTTP exporter.
func InitTracer(serviceNameParam, otlpEndpoint string) func(context.Context) error {
	serviceName = serviceNameParam

	exporter, err := otlptracehttp.New(context.Background(),
		otlptracehttp.WithEndpoint(otlpEndpoint),
		otlptracehttp.WithInsecure(),
	)
	if err != nil {
		panic(err)
	}

	// Resource with service name attribute
	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
		),
	)
	if err != nil {
		panic(err)
	}

	// Tracer provider with batch exporter and resource
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(provider)

	return provider.Shutdown
}

// StartSpan starts a new span with the given name using the global tracer.
// Returns an updated context containing the span and the span itself.
func StartSpan(ctx context.Context, name string) (context.Context, trace.Span) {
	tracer := otel.Tracer(serviceName)
	return tracer.Start(ctx, name)
}

// HTTPMiddleware returns an HTTP middleware that starts a span for each request,
// annotating common HTTP attributes. It assumes chi.RequestID and chi.RealIP
// are applied earlier in the chain.
func HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Start span
		ctx, span := StartSpan(r.Context(), r.Method+" "+r.URL.Path)
		defer span.End()

		// Set common attributes
		span.SetAttributes(
			semconv.HTTPMethodKey.String(r.Method),
			semconv.HTTPTargetKey.String(r.URL.Path),
			attribute.String("http.request_id", middleware.GetReqID(r.Context())),
			attribute.String("http.client_ip", r.RemoteAddr),
		)

		// Wrap response writer to capture status
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r.WithContext(ctx))

		// Record status code and latency
		span.SetAttributes(
			semconv.HTTPStatusCodeKey.Int(ww.Status()),
			attribute.Float64("http.duration_ms", float64(time.Since(start).Milliseconds())),
		)
	})
}
