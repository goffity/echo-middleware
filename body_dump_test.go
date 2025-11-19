package echomiddleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestBodyDumpLogsSanitizedPayload(t *testing.T) {
	viper.Set("ENVIRONMENT", "development")
	t.Cleanup(func() {
		viper.Set("ENVIRONMENT", "")
	})

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api", strings.NewReader("ignored"))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.RemoteAddr = "10.0.0.1:4000"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api")
	c.Response().Status = http.StatusAccepted

	core, obs := observer.New(zapcore.InfoLevel)
	logger := zap.New(core)
	undo := zap.ReplaceGlobals(logger)
	t.Cleanup(func() { undo() })

	reqBody := "{\n\t\"foo\":\"bar\"\r\n}"
	resBody := "first\nsecond\t"
	BodyDump(c, []byte(reqBody), []byte(resBody))

	entries := obs.All()
	require.Len(t, entries, 1)
	require.True(t, strings.HasPrefix(entries[0].Message, "Body dump: "))

	payload := strings.TrimPrefix(entries[0].Message, "Body dump: ")
	var model BodyDumpModel
	require.NoError(t, json.Unmarshal([]byte(payload), &model))

	assert.Equal(t, "example.com", model.Host)
	assert.Equal(t, "/api", model.Path)
	assert.Equal(t, http.MethodPost, model.Method)
	assert.Equal(t, req.RemoteAddr, model.RemoteAddress)
	assert.Equal(t, http.StatusAccepted, model.Status)
	assert.Equal(t, "{\"foo\":\"bar\"}", model.Request)
	assert.Equal(t, "firstsecond", model.Response)
	assert.Contains(t, model.Header, "Content-Type")
}

func TestBodyDumpSkipsLoggingInProductionAndHealthz(t *testing.T) {
	cases := []struct {
		name string
		env  string
		path string
	}{
		{name: "production-env", env: "production", path: "/api"},
		{name: "healthz-path", env: "development", path: "/healthz"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			viper.Set("ENVIRONMENT", tc.env)
			defer viper.Set("ENVIRONMENT", "")

			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetPath(tc.path)

			core, obs := observer.New(zapcore.InfoLevel)
			logger := zap.New(core)
			undo := zap.ReplaceGlobals(logger)
			defer undo()

			BodyDump(c, []byte("req"), []byte("res"))
			assert.Len(t, obs.All(), 0)
		})
	}
}
