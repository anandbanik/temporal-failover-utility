package temporal

import (
	"go.temporal.io/sdk/client"
	"go.uber.org/zap"

	"temporal-utility/config"
)

func NewClient(cfg *config.TemporalConfig, logger *zap.Logger) (client.Client, error) {
	c, err := client.Dial(client.Options{
		HostPort: cfg.HostPort,
	})
	if err != nil {
		logger.Error("failed to connect to Temporal", zap.String("host_port", cfg.HostPort), zap.Error(err))
		return nil, err
	}
	logger.Info("connected to Temporal", zap.String("host_port", cfg.HostPort))
	return c, nil
}
