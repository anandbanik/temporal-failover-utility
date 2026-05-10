package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func newObservedLogger() (*zap.Logger, *observer.ObservedLogs) {
	core, logs := observer.New(zapcore.DebugLevel)
	return zap.New(core), logs
}

func newLoggerEngine(logger *zap.Logger, statusCode int) *gin.Engine {
	r := gin.New()
	r.Use(Logger(logger))
	r.GET("/test", func(c *gin.Context) {
		c.Status(statusCode)
	})
	return r
}

func TestLogger_LogsRequestFields(t *testing.T) {
	logger, logs := newObservedLogger()
	r := newLoggerEngine(logger, http.StatusOK)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(httptest.NewRecorder(), req)

	require.Equal(t, 1, logs.Len())
	entry := logs.All()[0]
	fields := make(map[string]any, len(entry.Context))
	for _, f := range entry.Context {
		fields[f.Key] = f.String
	}

	assert.Equal(t, "GET", fields["method"])
	assert.Equal(t, "/test", fields["path"])
}

func TestLogger_InfoLevel_2xx(t *testing.T) {
	logger, logs := newObservedLogger()
	r := newLoggerEngine(logger, http.StatusOK)

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/test", nil))

	require.Equal(t, 1, logs.Len())
	assert.Equal(t, zapcore.InfoLevel, logs.All()[0].Level)
}

func TestLogger_WarnLevel_4xx(t *testing.T) {
	logger, logs := newObservedLogger()
	r := gin.New()
	r.Use(Logger(logger))
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusNotFound) })

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/test", nil))

	require.Equal(t, 1, logs.Len())
	assert.Equal(t, zapcore.WarnLevel, logs.All()[0].Level)
}

func TestLogger_ErrorLevel_5xx(t *testing.T) {
	logger, logs := newObservedLogger()
	r := gin.New()
	r.Use(Logger(logger))
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusInternalServerError) })

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/test", nil))

	require.Equal(t, 1, logs.Len())
	assert.Equal(t, zapcore.ErrorLevel, logs.All()[0].Level)
}

func TestLogger_StatusAndLatencyPresent(t *testing.T) {
	logger, logs := newObservedLogger()
	r := newLoggerEngine(logger, http.StatusCreated)

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/test", nil))

	require.Equal(t, 1, logs.Len())
	entry := logs.All()[0]

	var hasStatus, hasLatency bool
	for _, f := range entry.Context {
		if f.Key == "status" {
			hasStatus = true
		}
		if f.Key == "latency" {
			hasLatency = true
		}
	}
	assert.True(t, hasStatus, "expected status field in log")
	assert.True(t, hasLatency, "expected latency field in log")
}
