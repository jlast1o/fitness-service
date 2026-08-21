package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/redis/go-redis/v9"

	"fitness-platform/pkg/config"
	"fitness-platform/pkg/logger"
	"fitness-platform/pkg/shutdown"
	"fitness-platform/services/planner/internal/consumer"
	"fitness-platform/services/planner/internal/database"
	"fitness-platform/services/planner/internal/handler"
	"fitness-platform/services/planner/internal/repository/postgres"
	"fitness-platform/services/planner/internal/server"
	"fitness-platform/services/planner/internal/service"
)

func main() {
	logger.Init("info")

	cfg, err := config.Load()
	if err != nil {
		logger.Log.Fatal().Err(err).Msg("Failed to load config")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Log.Fatal().Err(err).Msg("Failed to connect to database")
	}
	defer pool.Close()

	if err := runMigrations(cfg.DatabaseURL); err != nil {
		logger.Log.Fatal().Err(err).Msg("Failed to run migrations")
	}

	plannerRepo := postgres.NewPlannerRepo(pool)
	plannerService := service.NewPlannerService(plannerRepo)
	plannerHandler := handler.NewPlannerHandler(plannerService)

	httpShutdown, err := server.RunREST(fmt.Sprintf(":%s", cfg.HTTPPort), plannerHandler, cfg.JWTSecret)
	if err != nil {
		logger.Log.Fatal().Err(err).Msg("Failed to start HTTP server")
	}

	redisClient := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	if err := redisClient.Ping(ctx).Err(); err != nil {
		logger.Log.Fatal().Err(err).Msg("Failed to connect to Redis")
	}
	defer redisClient.Close()

	plannerConsumer := consumer.NewRedisConsumer(
		redisClient,
		"workout.events",
		"planner-group",
		"planner-consumer-1",
		plannerService,
	)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		plannerConsumer.Run(ctx)
	}()

	shutdown.Graceful(ctx, cancel,
		httpShutdown,
		func(ctx context.Context) error {
			wg.Wait()
			return nil
		},
	)
}

func runMigrations(databaseURL string) error {
	m, err := migrate.New("file://migrations", databaseURL)
	if err != nil {
		return fmt.Errorf("failed to create migrate instance: %w", err)
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("failed to apply migrations: %w", err)
	}
	return nil
}
