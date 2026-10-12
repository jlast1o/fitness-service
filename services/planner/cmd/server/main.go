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
	"fitness-platform/services/planner/internal/consumer"
	"fitness-platform/services/planner/internal/database"
	"fitness-platform/services/planner/internal/handler"
	"fitness-platform/services/planner/internal/repository/postgres"
	"fitness-platform/services/planner/internal/server"
	"fitness-platform/services/planner/internal/service"
)

func main() {
	logger.Init("planner", "info")

	cfg, err := config.Load()
	if err != nil {
		logger.Log.Fatal().
			Err(err).
			Msg("Failed to load config")
	}

	// Корневой контекст.
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	defer cancel()

	// Контекст получения сообщений.
	pollCtx, stopPolling := context.WithCancel(ctx)
	defer stopPolling()

	// Контекст активной обработки.
	workCtx, cancelWork := context.WithCancel(ctx)
	defer cancelWork()

	tracingShutdown, err := tracing.Init(
		ctx,
		"planner",
		cfg.OTLPEndpoint,
	)
	if err != nil {
		logger.Log.Fatal().
			Err(err).
			Msg("Failed to initialize tracing")
	}

	// PostgreSQL.
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

	// Миграции.
	if err := runMigrations(cfg.DatabaseURL); err != nil {
		logger.Log.Fatal().
			Err(err).
			Msg("Failed to run migrations")
	}

	// Бизнес-слои.
	plannerRepo := postgres.NewPlannerRepo(
		pool,
		cfg.DatabaseOperationTimeout,
	)

	plannerService := service.NewPlannerService(
		plannerRepo,
	)

	plannerHandler := handler.NewPlannerHandler(
		plannerService,
	)

	// Redis.
	redisClient := redis.NewClient(&redis.Options{
		Addr: cfg.RedisAddr,
	})
	defer redisClient.Close()

	// HTTP-сервер.
	httpShutdown, err := server.RunREST(
		fmt.Sprintf(":%s", cfg.HTTPPort),
		plannerHandler,
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

	// Redis Consumer.
	plannerConsumer := consumer.NewRedisConsumer(
		redisClient,
		"workout.events",
		"planner-group",
		"planner-consumer-1",
		plannerService,
		cfg.RedisConsumerClaimMinIdle,
		cfg.RedisConsumerClaimCount,
	)

	consumerDone := make(chan struct{})

	go func() {
		defer close(consumerDone)

		plannerConsumer.RunWithContexts(
			pollCtx,
			workCtx,
		)
	}()

	// Управляемое завершение.
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
