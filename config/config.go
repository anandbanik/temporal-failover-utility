package config

import (
	"os"
	"strconv"
)

type Config struct {
	Server   ServerConfig
	Temporal TemporalConfig
	OTEL     OTELConfig
}

type ServerConfig struct {
	Port string
}

type TemporalConfig struct {
	HostPort string
}

type OTELConfig struct {
	ExporterEndpoint string
	ServiceName      string
	ServiceVersion   string
}

func Load() *Config {
	return &Config{
		Server: ServerConfig{
			Port: getEnv("SERVER_PORT", "9090"),
		},
		Temporal: TemporalConfig{
			HostPort: getEnv("TEMPORAL_HOST_PORT", "localhost:7233"),
		},
		OTEL: OTELConfig{
			ExporterEndpoint: getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
			ServiceName:      getEnv("OTEL_SERVICE_NAME", "temporal-utility"),
			ServiceVersion:   getEnv("OTEL_SERVICE_VERSION", "0.1.0"),
		},
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

var _ = getEnvInt // suppress unused warning; exported for future use
