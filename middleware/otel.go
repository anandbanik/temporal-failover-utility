package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

const instrumentationName = "temporal-utility/middleware"

// OTEL instruments HTTP server spans, request counts, and latency histograms.
func OTEL() gin.HandlerFunc {
	tracer := otel.Tracer(instrumentationName)
	meter := otel.Meter(instrumentationName)

	reqCounter, _ := meter.Int64Counter(
		"http.server.request.count",
		metric.WithDescription("Total number of HTTP requests"),
		metric.WithUnit("{request}"),
	)
	reqDuration, _ := meter.Float64Histogram(
		"http.server.request.duration",
		metric.WithDescription("HTTP request latency in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5),
	)

	return func(c *gin.Context) {
		start := time.Now()

		ctx, span := tracer.Start(c.Request.Context(), c.FullPath(),
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				semconv.HTTPRequestMethodKey.String(c.Request.Method),
				semconv.URLPath(c.Request.URL.Path),
			),
		)
		defer span.End()

		c.Request = c.Request.WithContext(ctx)
		c.Next()

		status := c.Writer.Status()
		attrs := []attribute.KeyValue{
			semconv.HTTPRequestMethodKey.String(c.Request.Method),
			semconv.HTTPRouteKey.String(c.FullPath()),
			semconv.HTTPResponseStatusCode(status),
		}

		span.SetAttributes(semconv.HTTPResponseStatusCode(status))

		reqCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
		reqDuration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(attrs...))
	}
}
