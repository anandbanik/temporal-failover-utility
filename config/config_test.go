package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func unsetEnvVars(t *testing.T, keys ...string) {
	t.Helper()
	for _, k := range keys {
		orig, exists := os.LookupEnv(k)
		os.Unsetenv(k) //nolint:errcheck
		if exists {
			t.Cleanup(func() { os.Setenv(k, orig) }) //nolint:errcheck
		}
	}
}

func TestLoad_Defaults(t *testing.T) {
	unsetEnvVars(t,
		"SERVER_PORT",
		"TEMPORAL_HOST_PORT",
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_SERVICE_NAME",
		"OTEL_SERVICE_VERSION",
	)

	cfg := Load()

	assert.Equal(t, "9090", cfg.Server.Port)
	assert.Equal(t, "localhost:7233", cfg.Temporal.HostPort)
	assert.Equal(t, "", cfg.OTEL.ExporterEndpoint)
	assert.Equal(t, "temporal-utility", cfg.OTEL.ServiceName)
	assert.Equal(t, "0.1.0", cfg.OTEL.ServiceVersion)
}

func TestLoad_EnvOverrides(t *testing.T) {
	t.Setenv("SERVER_PORT", "9090")
	t.Setenv("TEMPORAL_HOST_PORT", "temporal.prod:7233")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "collector:4317")
	t.Setenv("OTEL_SERVICE_NAME", "my-service")
	t.Setenv("OTEL_SERVICE_VERSION", "1.2.3")

	cfg := Load()

	assert.Equal(t, "9090", cfg.Server.Port)
	assert.Equal(t, "temporal.prod:7233", cfg.Temporal.HostPort)
	assert.Equal(t, "collector:4317", cfg.OTEL.ExporterEndpoint)
	assert.Equal(t, "my-service", cfg.OTEL.ServiceName)
	assert.Equal(t, "1.2.3", cfg.OTEL.ServiceVersion)
}
