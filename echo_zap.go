package echomiddleware

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.opentelemetry.io/otel/trace"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type responseWriter struct {
	io.Writer
	http.ResponseWriter
}

func (w *responseWriter) Write(b []byte) (int, error) {
	return w.Writer.Write(b)
}
func ZapLogger(log *zap.Logger, collection *mongo.Collection) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {

			if websocket.IsWebSocketUpgrade(c.Request()) {
				return next(c)
			}

			start := time.Now()

			req := c.Request()

			var (
				bodyBytes []byte
				err       error
			)
			if !websocket.IsWebSocketUpgrade(req) {
				bodyBytes, err = readAndResetBody(req)
				if err != nil {
					return err
				}
			}

			resBody := new(bytes.Buffer)
			mw := io.MultiWriter(c.Response().Writer, resBody)
			writer := &responseWriter{Writer: mw, ResponseWriter: c.Response().Writer}

			if !websocket.IsWebSocketUpgrade(req) {
				c.Response().Writer = writer
			}

			err = next(c)
			if err != nil {
				c.Error(err)
			}

			res := c.Response()

			requestID := req.Header.Get(echo.HeaderXRequestID)
			if requestID == "" {
				requestID = res.Header().Get(echo.HeaderXRequestID)
			}

			span := GetSpanFromContext(c.Request().Context())
			tracerID := GetTraceIDFromContext(c.Request().Context())
			spanID := span.SpanContext().SpanID().String()

			params := fmt.Sprintf("%v", c.ParamValues())

			fields := []zapcore.Field{
				zap.Int("status", res.Status),
				zap.String("latency", time.Since(start).String()),
				zap.String("request_id", requestID),
				zap.String("trace_id", tracerID),
				zap.String("span_id", spanID),
				zap.String("time", time.Now().Format(time.RFC3339)),
				zap.Int64("timestamp", time.Now().Unix()),
				zap.String("method", req.Method),
				zap.String("uri", req.RequestURI),
				zap.String("host", req.Host),
				zap.String("remote_ip", c.RealIP()),
				zap.String("header", fmt.Sprintf("%v", req.Header)),
				zap.String("path", c.Path()),
				zap.String("query", c.QueryString()),
				zap.String("form", req.Form.Encode()),
				zap.String("param", params),
				zap.String("body", string(bodyBytes)),
				zap.String("user_agent", req.UserAgent()),
				zap.String("referer", req.Referer()),
				zap.String("request_proto", req.Proto),
				zap.String("response", resBody.String()),
			}

			if c.Path() == "/healthz" && res.Status == 200 {
				return nil
			}

			n := res.Status
			switch {
			case n >= 500:
				log.Error("Server error", fields...)
			case n >= 400:
				log.Warn("Client error", fields...)
			case n >= 300:
				log.Info("Redirection", fields...)
			default:
				log.Info("Success", fields...)
			}

			if collection != nil {
				go func(fields []zapcore.Field) {
					fieldMap := zapFieldsToMap(fields)

					insertCtx, insertCancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer insertCancel()
					if err := mongoInsertFunc(insertCtx, collection, fieldMap); err != nil {
						log.Error("Error while inserting log to mongo", zap.Error(err))
					}

				}(fields)
			}

			return nil
		}
	}
}

func GetSpanFromContext(ctx context.Context) trace.Span {
	return trace.SpanFromContext(ctx)
}

func readAndResetBody(req *http.Request) ([]byte, error) {
	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	return bodyBytes, nil
}

var mongoInsertFunc = func(ctx context.Context, collection *mongo.Collection, document interface{}) error {
	if collection == nil {
		return fmt.Errorf("collection is nil")
	}
	_, err := collection.InsertOne(ctx, document)
	return err
}

func zapFieldsToMap(fields []zapcore.Field) map[string]interface{} {
	fieldMap := make(map[string]interface{}, len(fields))
	for _, field := range fields {
		switch field.Type {
		case zapcore.StringType:
			fieldMap[field.Key] = field.String
		case zapcore.Int64Type, zapcore.Int32Type, zapcore.Int16Type, zapcore.Int8Type, zapcore.Uint64Type, zapcore.Uint32Type, zapcore.Uint16Type, zapcore.Uint8Type:
			fieldMap[field.Key] = field.Integer
		case zapcore.Float64Type, zapcore.Float32Type:
			fieldMap[field.Key] = float64(field.Integer)
		case zapcore.BoolType:
			fieldMap[field.Key] = field.Integer != 0
		case zapcore.TimeType:
			fieldMap[field.Key] = time.Unix(0, field.Integer).Format(time.RFC3339)
		case zapcore.DurationType:
			fieldMap[field.Key] = field.Integer
		case zapcore.ReflectType:
			fieldMap[field.Key] = field.Interface
		default:
			fieldMap[field.Key] = field.String
		}
	}
	return fieldMap
}
