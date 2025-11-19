package echomiddleware

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/mongo"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func newTestContext(t *testing.T, method, target, body string) (*echo.Echo, echo.Context, *httptest.ResponseRecorder) {
	t.Helper()

	e := echo.New()
	req := httptest.NewRequest(method, target, bytes.NewBufferString(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.Header.Set("User-Agent", "unit-agent")
	req.Header.Set("Referer", "http://ref.example")
	req.Header.Set(echo.HeaderXRealIP, "10.0.0.1")
	req.Host = "example.local"
	req.Form = url.Values{"form": {"value"}}

	spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{0, 1, 2, 3, 4, 5, 6, 7, 7, 6, 5, 4, 3, 2, 1, 0},
		SpanID:     trace.SpanID{8, 7, 6, 5, 4, 3, 2, 1},
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(req.Context(), spanCtx)
	ctx = context.WithValue(ctx, traceIDContextKey, "trace-from-context")
	ctx = context.WithValue(ctx, requestIDContextKey, "req-from-context")
	ctx = context.WithValue(ctx, spanIDContextKey, "span-from-context")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/test/:id")
	c.SetParamNames("id")
	c.SetParamValues("123")

	return e, c, rec
}

func TestZapLoggerLogsSuccessAndRestoresBody(t *testing.T) {
	_, c, rec := newTestContext(t, http.MethodPost, "/test/123?foo=bar", "req-body")
	c.Request().Header.Set(echo.HeaderXRequestID, "req-header-id")

	core, obs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)

	middleware := ZapLogger(logger, nil)
	handler := middleware(func(c echo.Context) error {
		body, err := io.ReadAll(c.Request().Body)
		require.NoError(t, err)
		require.Equal(t, "req-body", string(body))
		c.Response().Header().Set(echo.HeaderXRequestID, "resp-id")
		return c.String(http.StatusCreated, "response-body")
	})

	require.NoError(t, handler(c))
	assert.Equal(t, http.StatusCreated, rec.Code)

	entries := obs.All()
	require.Len(t, entries, 1)
	entry := entries[0]
	assert.Equal(t, "Success", entry.Message)
	assert.Equal(t, zapcore.InfoLevel, entry.Level)

	contextFields := entry.ContextMap()
	assert.Equal(t, int64(http.StatusCreated), contextFields["status"])
	assert.Equal(t, "req-body", contextFields["body"])
	assert.Equal(t, "response-body", contextFields["response"])
	assert.Equal(t, "req-header-id", contextFields["request_id"])
	assert.Equal(t, "/test/:id", contextFields["path"])
	assert.Equal(t, "[123]", contextFields["param"])
	assert.Equal(t, "foo=bar", contextFields["query"])
	assert.Equal(t, "unit-agent", contextFields["user_agent"])
	assert.Equal(t, "HTTP/1.1", contextFields["request_proto"])
}

func TestZapLoggerRequestIDFallback(t *testing.T) {
	_, c, _ := newTestContext(t, http.MethodGet, "/test/123", "")

	core, obs := observer.New(zapcore.InfoLevel)
	logger := zap.New(core)

	middleware := ZapLogger(logger, nil)
	handler := middleware(func(c echo.Context) error {
		c.Response().Header().Set(echo.HeaderXRequestID, "generated")
		return c.NoContent(http.StatusOK)
	})

	require.NoError(t, handler(c))

	entries := obs.All()
	require.Len(t, entries, 1)
	assert.Equal(t, "generated", entries[0].ContextMap()["request_id"])
}

type errorReadCloser struct {
	err error
}

func (e errorReadCloser) Read([]byte) (int, error) {
	return 0, e.err
}

func (e errorReadCloser) Close() error { return nil }

func TestZapLoggerBodyReadError(t *testing.T) {
	_, c, _ := newTestContext(t, http.MethodPost, "/test/123", "")
	readErr := errors.New("read failed")
	c.Request().Body = errorReadCloser{err: readErr}

	middleware := ZapLogger(zap.NewNop(), nil)
	handler := middleware(func(c echo.Context) error {
		t.Fatal("handler should not be called when body read fails")
		return nil
	})

	err := handler(c)
	require.EqualError(t, err, readErr.Error())
}

func TestZapLoggerWebSocketUpgradeSkipsLogging(t *testing.T) {
	_, c, _ := newTestContext(t, http.MethodGet, "/test/123", "")
	c.Request().Header.Set("Connection", "Upgrade")
	c.Request().Header.Set("Upgrade", "websocket")

	core, obs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)

	calls := 0
	middleware := ZapLogger(logger, nil)
	handler := middleware(func(c echo.Context) error {
		calls++
		return c.NoContent(http.StatusSwitchingProtocols)
	})

	require.NoError(t, handler(c))
	assert.Equal(t, 1, calls)
	assert.Len(t, obs.All(), 0)
}

func TestZapLoggerHealthCheckSkipsLogging(t *testing.T) {
	_, c, _ := newTestContext(t, http.MethodGet, "/healthz", "")
	c.SetPath("/healthz")

	core, obs := observer.New(zapcore.InfoLevel)
	logger := zap.New(core)

	middleware := ZapLogger(logger, nil)
	handler := middleware(func(c echo.Context) error {
		return c.NoContent(http.StatusOK)
	})

	require.NoError(t, handler(c))
	assert.Len(t, obs.All(), 0)
}

func TestZapLoggerStatusBranches(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		message string
		level   zapcore.Level
	}{
		{name: "redirect", status: http.StatusTemporaryRedirect, message: "Redirection", level: zapcore.InfoLevel},
		{name: "client-error", status: http.StatusNotFound, message: "Client error", level: zapcore.WarnLevel},
		{name: "server-error", status: http.StatusInternalServerError, message: "Server error", level: zapcore.ErrorLevel},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, c, _ := newTestContext(t, http.MethodGet, "/test/123", "")

			core, obs := observer.New(zapcore.DebugLevel)
			logger := zap.New(core)

			middleware := ZapLogger(logger, nil)
			handler := middleware(func(c echo.Context) error {
				return c.String(tc.status, tc.name)
			})

			require.NoError(t, handler(c))
			entries := obs.All()
			require.Len(t, entries, 1)
			assert.Equal(t, tc.message, entries[0].Message)
			assert.Equal(t, tc.level, entries[0].Level)
		})
	}
}

func TestZapLoggerHandlerErrorInvokesEchoErrorHandler(t *testing.T) {
	e, c, _ := newTestContext(t, http.MethodGet, "/test/123", "")

	var captured error
	e.HTTPErrorHandler = func(err error, c echo.Context) {
		captured = err
	}

	middleware := ZapLogger(zap.NewNop(), nil)
	handler := middleware(func(c echo.Context) error {
		return errors.New("handler failed")
	})

	require.NoError(t, handler(c))
	require.EqualError(t, captured, "handler failed")
}

func TestZapLoggerMongoInsertion(t *testing.T) {
	_, c, _ := newTestContext(t, http.MethodPost, "/test/123", "body")

	var wg sync.WaitGroup
	wg.Add(1)

	var mu sync.Mutex
	collected := make(map[string]interface{})

	originalInsert := mongoInsertFunc
	t.Cleanup(func() { mongoInsertFunc = originalInsert })

	mongoInsertFunc = func(ctx context.Context, collection *mongo.Collection, document interface{}) error {
		defer wg.Done()
		mu.Lock()
		defer mu.Unlock()
		for k, v := range document.(map[string]interface{}) {
			collected[k] = v
		}
		return errors.New("insert failed")
	}

	core, obs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)

	middleware := ZapLogger(logger, &mongo.Collection{})
	handler := middleware(func(c echo.Context) error {
		return c.String(http.StatusInternalServerError, "boom")
	})

	require.NoError(t, handler(c))
	wg.Wait()

	entries := obs.All()
	require.Len(t, entries, 2)
	assert.Equal(t, "Server error", entries[0].Message)
	assert.Equal(t, "Error while inserting log to mongo", entries[1].Message)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, int64(http.StatusInternalServerError), collected["status"])
	assert.Equal(t, "boom", collected["response"])
	assert.Equal(t, "body", collected["body"])
}

func TestZapFieldsToMapCoversAllTypes(t *testing.T) {
	now := time.Unix(1, 0).UTC()
	fields := []zapcore.Field{
		zap.String("string", "value"),
		zap.Int64("int", 7),
		zap.Uint32("uint", 8),
		zap.Float64("float", 3.14),
		zap.Bool("bool", true),
		zap.Time("time", now),
		zap.Duration("duration", time.Second),
		zap.Reflect("reflect", map[string]int{"a": 1}),
		{Key: "default", String: "fallback"},
	}

	result := zapFieldsToMap(fields)
	assert.Equal(t, "value", result["string"])
	assert.Equal(t, int64(7), result["int"])
	assert.Equal(t, int64(8), result["uint"])
	assert.Equal(t, float64(math.Float64bits(3.14)), result["float"])
	assert.Equal(t, true, result["bool"])
	assert.Equal(t, time.Unix(0, now.UnixNano()).Format(time.RFC3339), result["time"])
	assert.Equal(t, int64(time.Second), result["duration"])
	assert.Equal(t, map[string]int{"a": 1}, result["reflect"])
	assert.Equal(t, "fallback", result["default"])
}

func TestGetSpanFromContext(t *testing.T) {
	spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 8, 7, 6, 5, 4, 3, 2, 1},
		SpanID:  trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8},
	})
	ctx := trace.ContextWithSpanContext(context.Background(), spanCtx)

	span := GetSpanFromContext(ctx)
	assert.Equal(t, spanCtx, span.SpanContext())
}
