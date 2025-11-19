package echomiddleware

import (
	"context"

	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Context keys for storing logger and IDs
// ใช้ string ตรงๆ แทน custom type เพื่อให้ repository layer เข้าถึงได้
const (
	loggerContextKey    = "logger"
	traceIDContextKey   = "trace_id"
	spanIDContextKey    = "span_id"
	requestIDContextKey = "request_id"
	// RequestIDAttribute is the span attribute key for request ID
	RequestIDAttribute = "request.id"
)

// OtelLoggerMiddleware is an Echo middleware that:
// 1. Sets request_id as a span attribute for OpenTelemetry tracing
// 2. Stores request_id in context for logger access
func OtelLoggerMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Get the current span from the request context
			span := trace.SpanFromContext(c.Request().Context())

			// Extract request ID from Echo's RequestID middleware
			requestID := c.Response().Header().Get(echo.HeaderXRequestID)
			if requestID == "" {
				requestID = c.Request().Header.Get(echo.HeaderXRequestID)
			}

			// Set request_id as a span attribute for distributed tracing
			if span.SpanContext().IsValid() {
				span.SetAttributes(attribute.String(RequestIDAttribute, requestID))
			}

			// Store request_id in context for logger access
			ctx := context.WithValue(c.Request().Context(), requestIDContextKey, requestID)
			c.SetRequest(c.Request().WithContext(ctx))

			return next(c)
		}
	}
}

// LoggerWithContext is an Echo middleware that injects trace_id, span_id, and request_id into the logger
// and stores the enhanced logger in the context for use across all layers (API -> Service -> Repository)
func LoggerWithContext() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Extract trace and span IDs from OpenTelemetry context
			span := trace.SpanFromContext(c.Request().Context())
			spanContext := span.SpanContext()

			traceID := ""
			spanID := ""
			if spanContext.IsValid() {
				traceID = spanContext.TraceID().String()
				spanID = spanContext.SpanID().String()
			}

			// Extract request ID from Echo's RequestID middleware
			requestID := c.Response().Header().Get(echo.HeaderXRequestID)
			if requestID == "" {
				// Fallback: try to get from request header
				requestID = c.Request().Header.Get(echo.HeaderXRequestID)
			}

			// Create a new logger with trace_id, span_id, and request_id fields
			logger := zap.S().With(
				"trace_id", traceID,
				"span_id", spanID,
				"request_id", requestID,
			)

			// Store the logger and IDs in Echo context for handler access
			c.Set(loggerContextKey, logger)
			c.Set(traceIDContextKey, traceID)
			c.Set(spanIDContextKey, spanID)
			c.Set(requestIDContextKey, requestID)

			// Store the logger in the standard Go context for service/repository layers
			ctx := context.WithValue(c.Request().Context(), loggerContextKey, logger)
			ctx = context.WithValue(ctx, traceIDContextKey, traceID)
			ctx = context.WithValue(ctx, spanIDContextKey, spanID)
			ctx = context.WithValue(ctx, requestIDContextKey, requestID)

			// Replace the request context with the enhanced context
			c.SetRequest(c.Request().WithContext(ctx))

			return next(c)
		}
	}
}

// GetLogger retrieves the logger with trace_id, span_id, and request_id from Echo context
// Use this in route handlers
func GetLogger(c echo.Context) *zap.SugaredLogger {
	if logger, ok := c.Get(loggerContextKey).(*zap.SugaredLogger); ok {
		return logger
	}
	// Fallback to global logger if context logger not found
	return zap.S()
}

// GetLoggerFromContext retrieves the logger with trace_id, span_id, and request_id from standard Go context
// Use this in service and repository layers
func GetLoggerFromContext(ctx context.Context) *zap.SugaredLogger {
	if logger, ok := ctx.Value(loggerContextKey).(*zap.SugaredLogger); ok {
		return logger
	}
	// Fallback to global logger if context logger not found
	return zap.S()
}

// GetTraceID retrieves the trace ID from Echo context
func GetTraceID(c echo.Context) string {
	if traceID, ok := c.Get(traceIDContextKey).(string); ok {
		return traceID
	}
	return ""
}

// GetTraceIDFromContext retrieves the trace ID from standard Go context
func GetTraceIDFromContext(ctx context.Context) string {
	if traceID, ok := ctx.Value(traceIDContextKey).(string); ok {
		return traceID
	}
	return ""
}

// GetSpanID retrieves the span ID from Echo context
func GetSpanID(c echo.Context) string {
	if spanID, ok := c.Get(spanIDContextKey).(string); ok {
		return spanID
	}
	return ""
}

// GetSpanIDFromContext retrieves the span ID from standard Go context
func GetSpanIDFromContext(ctx context.Context) string {
	if spanID, ok := ctx.Value(spanIDContextKey).(string); ok {
		return spanID
	}
	return ""
}

// GetRequestID retrieves the request ID from Echo context
func GetRequestID(c echo.Context) string {
	if requestID, ok := c.Get(requestIDContextKey).(string); ok {
		return requestID
	}
	return ""
}

// GetRequestIDFromContext retrieves the request ID from standard Go context
func GetRequestIDFromContext(ctx context.Context) string {
	if requestID, ok := ctx.Value(requestIDContextKey).(string); ok {
		return requestID
	}
	return ""
}
