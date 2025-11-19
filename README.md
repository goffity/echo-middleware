# Echo Middleware

Custom middleware helpers for the Echo framework that provide structured logging, request body dumping, and context-aware logger propagation.

## Features

- **Zap request logging**: `ZapLogger` captures request/response payloads, calculates latency, emits structured zap fields, and optionally persists entries to MongoDB.
- **Request body dump**: `BodyDump` sanitizes request/response bodies and logs them outside production or `/healthz` traffic.
- **Tracing-aware logger context**: `OtelLoggerMiddleware` and `LoggerWithContext` propagate trace/span/request IDs so handlers, services, and repositories can retrieve a sugared logger that already carries tracing metadata.

## Installation

```bash
go get github.com/goffity/echo-middleware
```

Import the package and compose the middleware in your Echo server:

```go
import (
    echomiddleware "github.com/goffity/echo-middleware"
    "github.com/labstack/echo/v4"
    "go.uber.org/zap"
)

func main() {
    e := echo.New()

    log, _ := zap.NewProduction()
    e.Use(echomiddleware.ZapLogger(log, nil)) // pass a mongo.Collection if you need persistence
    e.Use(echomiddleware.OtelLoggerMiddleware())
    e.Use(echomiddleware.LoggerWithContext())

    // ...
}
```

## Development

- Format: `go fmt ./...`
- Lint (optional): `golangci-lint run`
- Tests: `go test ./... -cover`

The repository keeps tests beside source files (e.g., `echo_zap_test.go`), so add new `*_test.go` files next to the code they verify.

## Security Note

Avoid logging secrets or authentication headers. The body dump helpers already remove control characters but you should still sanitize sensitive fields before passing them to the logger or persistence layer.
