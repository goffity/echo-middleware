package echomiddleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

func TestOtelLoggerMiddlewareStoresRequestIDFromResponse(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/resource", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Response().Header().Set(echo.HeaderXRequestID, "resp-id")

	spanCtx := testSpanContext()
	ctx := trace.ContextWithSpanContext(context.Background(), spanCtx)
	c.SetRequest(req.WithContext(ctx))

	var stored string
	handler := OtelLoggerMiddleware()(func(c echo.Context) error {
		ctx := c.Request().Context()
		stored, _ = ctx.Value(requestIDContextKey).(string)
		return c.NoContent(http.StatusOK)
	})

	require.NoError(t, handler(c))
	assert.Equal(t, "resp-id", stored)
}

func TestOtelLoggerMiddlewareFallsBackToRequestHeader(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/resource", nil)
	req.Header.Set(echo.HeaderXRequestID, "req-id")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := OtelLoggerMiddleware()(func(c echo.Context) error {
		ctx := c.Request().Context()
		value, _ := ctx.Value(requestIDContextKey).(string)
		assert.Equal(t, "req-id", value)
		return nil
	})

	require.NoError(t, handler(c))
}

func TestLoggerWithContextPopulatesContext(t *testing.T) {
	global := zap.NewExample()
	undo := zap.ReplaceGlobals(global)
	t.Cleanup(func() { undo() })

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/resource", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	spanCtx := testSpanContext()
	ctx := trace.ContextWithSpanContext(context.Background(), spanCtx)
	c.SetRequest(req.WithContext(ctx))
	c.Response().Header().Set(echo.HeaderXRequestID, "resp-id")

	var (
		logger *zap.SugaredLogger
		gotCtx context.Context
	)
	handler := LoggerWithContext()(func(c echo.Context) error {
		logger = GetLogger(c)
		gotCtx = c.Request().Context()

		traceID := spanCtx.TraceID().String()
		spanID := spanCtx.SpanID().String()

		assert.Equal(t, traceID, GetTraceID(c))
		assert.Equal(t, spanID, GetSpanID(c))
		assert.Equal(t, "resp-id", GetRequestID(c))
		return nil
	})

	require.NoError(t, handler(c))
	require.NotNil(t, logger)
	assert.Equal(t, logger, GetLoggerFromContext(gotCtx))
	assert.Equal(t, spanCtx.TraceID().String(), GetTraceIDFromContext(gotCtx))
	assert.Equal(t, spanCtx.SpanID().String(), GetSpanIDFromContext(gotCtx))
	assert.Equal(t, "resp-id", GetRequestIDFromContext(gotCtx))
}

func TestLoggerWithContextFallsBackWithoutSpan(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/resource", nil)
	req.Header.Set(echo.HeaderXRequestID, "req-id")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := LoggerWithContext()(func(c echo.Context) error {
		assert.Empty(t, GetTraceID(c))
		assert.Empty(t, GetSpanID(c))
		assert.Equal(t, "req-id", GetRequestID(c))
		return nil
	})

	require.NoError(t, handler(c))
}

func TestLoggerHelpersFallbackToGlobals(t *testing.T) {
	global := zap.NewExample()
	undo := zap.ReplaceGlobals(global)
	t.Cleanup(func() { undo() })

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	logger := GetLogger(c)
	require.NotNil(t, logger)
	assert.Equal(t, global, logger.Desugar())

	ctx := context.Background()
	ctxLogger := GetLoggerFromContext(ctx)
	require.NotNil(t, ctxLogger)
	assert.Equal(t, global, ctxLogger.Desugar())

	assert.Empty(t, GetTraceID(c))
	assert.Empty(t, GetTraceIDFromContext(ctx))
	assert.Empty(t, GetSpanID(c))
	assert.Empty(t, GetSpanIDFromContext(ctx))
	assert.Empty(t, GetRequestID(c))
	assert.Empty(t, GetRequestIDFromContext(ctx))
}

func testSpanContext() trace.SpanContext {
	return trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 8, 7, 6, 5, 4, 3, 2, 1},
		SpanID:     trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8},
		TraceFlags: trace.FlagsSampled,
	})
}
