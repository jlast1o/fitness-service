package main

import (
	"context"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/redis/go-redis/v9"

	"fitness-platform/pkg/config"
	"fitness-platform/pkg/logger"
	"fitness-platform/pkg/shutdown"
	"fitness-platform/pkg/tracing"
	"fitness-platform/services/analytics/internal/consumer"
	"fitness-platform/services/analytics/internal/database"
	"fitness-platform/services/analytics/internal/handler"
	"fitness-platform/services/analytics/internal/repository/postgres"
	"fitness-platform/services/analytics/internal/server"
	"fitness-platform/services/analytics/internal/service"
)

func main() {
	// 1. Инициализация логгера.
	logger.Init("analytics", "info")

	// 2. Загружаем конфигурацию.
	cfg, err := config.Load()
	if err != nil {
		logger.Log.Fatal().
			Err(err).
			Msg("Failed to load config")
	}

	// 3. Создаём три контекста.
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	defer cancel()

	pollCtx, stopPolling := context.WithCancel(ctx)
	defer stopPolling()

	workCtx, cancelWork := context.WithCancel(ctx)
	defer cancelWork()

	// 4. Инициализируем трассировку.
	tracingShutdown, err := tracing.Init(
		ctx,
		"analytics",
		cfg.OTLPEndpoint,
	)
	if err != nil {
		logger.Log.Fatal().
			Err(err).
			Msg("Failed to initialize tracing")
	}

	// 5. Подключаем PostgreSQL.
	pool, err := database.NewPool(
		ctx,
		cfg.DatabaseURL,
		cfg.DatabaseConnectTimeout,
		cfg.DatabaseOperationTimeout,
	)
	if err != nil {
		logger.Log.Fatal().
			Err(err).
			Msg("Failed to connect to database")
	}
	defer pool.Close()

	// 6. Применяем миграции.
	if err := runMigrations(cfg.DatabaseURL); err != nil {
		logger.Log.Fatal().
			Err(err).
			Msg("Failed to run migrations")
	}

	// 7. Создаём Repository, Service и Handler.
	analyticsRepo := postgres.NewAnalyticsRepo(
		pool,
		cfg.DatabaseOperationTimeout,
	)

	analyticsService := service.NewAnalyticsService(
		analyticsRepo,
	)

	analyticsHandler := handler.NewAnalyticsHandler(
		analyticsService,
	)

	// 8. Создаём Redis Client.
	redisClient := redis.NewClient(&redis.Options{
		Addr: cfg.RedisAddr,
	})
	defer redisClient.Close()

	// 9. Запускаем HTTP-сервер.
	httpShutdown, err := server.RunREST(
		fmt.Sprintf(":%s", cfg.HTTPPort),
		analyticsHandler,
		cfg.JWTSecret,
		cfg.ReadinessTimeout,
		func(ctx context.Context) error {
			return pool.Ping(ctx)
		},
	)
	if err != nil {
		logger.Log.Fatal().
			Err(err).
			Msg("Failed to start HTTP server")
	}

	// 10. Создаём Redis Consumer.
	redisConsumer := consumer.NewRedisConsumer(
		redisClient,
		"workout.events",
		"analytics-group",
		"analytics-consumer-1",
		analyticsService,
		cfg.RedisConsumerClaimMinIdle,
		cfg.RedisConsumerClaimCount,
	)

	// Канал фактического завершения consumer.
	consumerDone := make(chan struct{})

	go func() {
		defer close(consumerDone)

		redisConsumer.RunWithContexts(
			pollCtx,
			workCtx,
		)
	}()

	// 11. Ожидаем SIGTERM / SIGINT и завершаемся.
	if err := shutdown.GracefulDrain(
		stopPolling,
		cancelWork,
		consumerDone,
		httpShutdown,
		tracingShutdown,
	); err != nil {
		logger.Log.Error().
			Err(err).
			Msg("incomplete graceful shutdown")
	}
}

// runMigrations применяет миграции PostgreSQL.
func runMigrations(databaseURL string) error {
	m, err := migrate.New(
		"file://migrations",
		databaseURL,
	)
	if err != nil {
		return fmt.Errorf(
			"failed to create migrate instance: %w",
			err,
		)
	}

	if err := m.Up(); err != nil &&
		err != migrate.ErrNoChange {
		return fmt.Errorf(
			"failed to apply migrations: %w",
			err,
		)
	}

	return nil
}
