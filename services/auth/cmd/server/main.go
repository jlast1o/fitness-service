package main

import (
	"context"
	"fitness-platform/pkg/config"
	"fitness-platform/pkg/logger"
	"fitness-platform/pkg/shutdown"
	"fitness-platform/services/auth/internal/database"
	"fitness-platform/services/auth/internal/handler"
	"fitness-platform/services/auth/internal/repository/postgres"
	"fitness-platform/services/auth/internal/server"
	"fitness-platform/services/auth/internal/service"
	"fmt"

	"github.com/golang-migrate/migrate"
)

func main() {
	logger.Init("info")

	cfg, err := config.Load()
	if err != nil {
		logger.Log.Fatal().Err(err).Msg("Failed to load cfg")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Log.Fatal().Err(err).Msg("failed to create connection pool")
	}
	defer pool.Close()

	if err := runMigrations(cfg.DatabaseURL); err != nil {
		logger.Log.Fatal().Err(err).Msg("Failed to run migrations")
	}

	userRepo := postgres.NewUserRepo(pool)

	authService := service.NewAuthService(userRepo, cfg.JWTSecret, cfg.JWTAccessTTL, cfg.JWTRefreshTTL)

	authHandler := handler.NewAuthHandler(authService)

	httpShutdown, err := server.RunREST(fmt.Sprintf(":%s", cfg.HTTPPort), authHandler)
	if err != nil {
		logger.Log.Fatal().Err(err).Msg("Failed to start HTTP server")
	}

	shutdown.Graceful(ctx, cancel, httpShutdown)
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
