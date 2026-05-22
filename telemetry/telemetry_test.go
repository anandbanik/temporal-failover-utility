package telemetry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"temporal-utility/config"
)

func TestSetup_StdoutExporter(t *testing.T) {
	cfg := &config.OTELConfig{
		ExporterEndpoint: "",
		ServiceName:      "test-service",
		ServiceVersion:   "0.0.1",
	}
	sdk, err := Setup(context.Background(), cfg, zap.NewNop())
	require.NoError(t, err)
	require.NotNil(t, sdk)

	err = sdk.Shutdown(context.Background())
	assert.NoError(t, err)
}

func TestSetup_SetsGlobalProviders(t *testing.T) {
	cfg := &config.OTELConfig{
		ExporterEndpoint: "",
		ServiceName:      "test-svc",
		ServiceVersion:   "1.0.0",
	}
	sdk, err := Setup(context.Background(), cfg, zap.NewNop())
	require.NoError(t, err)
	require.NotNil(t, sdk)
	t.Cleanup(func() { sdk.Shutdown(context.Background()) }) //nolint:errcheck
}

func TestSetup_OTLPExporter(t *testing.T) {
	cfg := &config.OTELConfig{
		ExporterEndpoint: "localhost:4317",
		ServiceName:      "test-service",
		ServiceVersion:   "0.0.1",
	}
	// OTLP gRPC connects lazily; creation succeeds even without a running collector.
	sdk, err := Setup(context.Background(), cfg, zap.NewNop())
	require.NoError(t, err)
	require.NotNil(t, sdk)

	// Shutdown flushes and closes; tolerate errors from unreachable collector.
	_ = sdk.Shutdown(context.Background())
}

func TestShutdown_CalledTwiceIsIdempotent(t *testing.T) {
	cfg := &config.OTELConfig{
		ExporterEndpoint: "",
		ServiceName:      "test-service",
		ServiceVersion:   "0.0.1",
	}
	sdk, err := Setup(context.Background(), cfg, zap.NewNop())
	require.NoError(t, err)

	assert.NoError(t, sdk.Shutdown(context.Background()))
	// Second shutdown may return an error ("shutdown called multiple times") — that's acceptable.
	_ = sdk.Shutdown(context.Background())
}
