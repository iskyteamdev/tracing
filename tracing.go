package tracing

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
)

// InitTracer configures the global Otel TracerProvider with a Jaeger exporter.
func InitTracer(serviceName, jaegerURL string) func(context.Context) error {
	// Создаём экспортёр Jaeger
	exporter, err := jaeger.New(
		jaeger.WithCollectorEndpoint(jaeger.WithEndpoint(jaegerURL)),
	)
	if err != nil {
		panic(err)
	}

	// Ресурс с атрибутом service.name
	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
		),
	)
	if err != nil {
		panic(err)
	}

	// Трейсер-провайдер с batch-экспортёром и ресурсом
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(provider)

	return provider.Shutdown
}

// Middleware returns an HTTP middleware that starts a span for each request,
// annotating common HTTP attributes. It assumes chi.RequestID and chi.RealIP
// are applied earlier in the chain.
func Middleware(next http.Handler) http.Handler {
	tracer := otel.Tracer("http-server")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Start span
		ctx, span := tracer.Start(r.Context(), r.Method+" "+r.URL.Path)
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
