package main

import (
	"context"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"

	"fitness-platform/pkg/config"
	"fitness-platform/pkg/logger"
	"fitness-platform/pkg/shutdown"
	"fitness-platform/pkg/tracing"
	"fitness-platform/services/nutrition/internal/database"
	"fitness-platform/services/nutrition/internal/server"
)

func main() {
	logger.Init("nutrition", "info")

	cfg, err := config.Load()
	if err != nil {
		logger.Log.Fatal().
			Err(err).
			Msg("Failed to load config")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tracingShutdown, err := tracing.Init(
		ctx,
		"nutrition",
		cfg.OTLPEndpoint,
	)
	if err != nil {
		logger.Log.Fatal().
			Err(err).
			Msg("Failed to initialize tracing")
	}

	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err := runMigrations(cfg.DatabaseURL); err != nil {
		logger.Log.Fatal().
			Err(err).
			Msg("Failed to run migrations")
	}

	httpShutdown, err := server.RunREST(
		fmt.Sprintf(":%s", cfg.HTTPPort),

		func(ctx context.Context) error {
			return pool.Ping(ctx)
		},
	)
	if err != nil {
		logger.Log.Fatal().
			Err(err).
			Msg("Failed to start HTTP server")
	}

	shutdown.Graceful(
		ctx,
		cancel,
		httpShutdown,
		tracingShutdown,
	)
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
