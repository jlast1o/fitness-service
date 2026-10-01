package config

import (
	"time"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	HTTPPort                 string        `envconfig:"HTTP_PORT" default:"8080"`
	GRPCPort                 string        `envconfig:"GRPC_PORT" default:"50051"`
	DatabaseURL              string        `envconfig:"DATABASE_URL" required:"true"`
	DatabaseConnectTimeout   time.Duration `envconfig:"DATABASE_CONNECT_TIMEOUT" default:"1s"` // таймаут подключения к базе данных при старте приложения
	DatabaseOperationTimeout time.Duration `envconfig:"DATABASE_OPERATION_TIMEOUT" default:"2s"`

	JWTSecret     string        `envconfig:"JWT_SECRET" required:"true"`
	JWTAccessTTL  time.Duration `envconfig:"JWT_ACCESS_TTL" default:"15m"`
	JWTRefreshTTL time.Duration `envconfig:"JWT_REFRESH_TTL" default:"72h"`

	RedisAddr        string        `envconfig:"REDIS_ADDR" default:"localhost:6379"`
	ExerciseCacheTTL time.Duration `envconfig:"EXERCISE_CACHE_TTL" default:"10m"`

	RedisDialTimeout  time.Duration `envconfig:"REDIS_DIAL_TIMEOUT" default:"500ms"`
	RedisReadTimeout  time.Duration `envconfig:"REDIS_READ_TIMEOUT" default:"500ms"`
	RedisWriteTimeout time.Duration `envconfig:"REDIS_WRITE_TIMEOUT" default:"500ms"`

	RedisCacheTimeout time.Duration `envconfig:"REDIS_CACHE_TIMEOUT" default:"750ms"`

	RedisMaxRetries  int `envconfig:"REDIS_MAX_RETRIES" default:"-1"` // отключаем автоматические повторные попытки, чтобы не блокировать обработку запросов
	RedisDialRetries int `envconfig:"REDIS_DIAL_RETRIES" default:"1"` // количество попыток подключения к Redis при старте приложения (0 = default, default = 5)

	OTLPEndpoint string `envconfig:"TRACING_OTLP_ENDPOINT" default:"localhost:4317"`
}

func Load() (*Config, error) {
	var cfg Config
	err := envconfig.Process("", &cfg)
	return &cfg, err
}
