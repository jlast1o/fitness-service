package config

import (
	"time"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	HTTPPort    string `envconfig:"HTTP_PORT" default:"8080"`
	GRPCPort    string `envconfig:"GRPC_PORT" default:"50051"`
	DatabaseURL string `envconfig:"DATABASE_URL" required:"true"`

	JWTSecret     string        `envconfig:"JWT_SECRET" required:"true"`
	JWTAccessTTL  time.Duration `envconfig:"JWT_ACCESS_TTL" default:"15m"`
	JWTRefreshTTL time.Duration `envconfig:"JWT_REFRESH_TTL" default:"72h"`
}

func Load() (*Config, error) {
	var cfg Config
	err := envconfig.Process("", &cfg)
	return &cfg, err
}
