package temporal

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"temporal-utility/config"
)

// Temporal SDK dials eagerly; without a running server the dial fails immediately.
// This covers the error path in NewClient.
func TestNewClient_ConnectionRefused(t *testing.T) {
	cfg := &config.TemporalConfig{HostPort: "localhost:9999"}
	c, err := NewClient(cfg, zap.NewNop())
	require.Error(t, err)
	assert.Nil(t, c)
}
