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

	"fitness-platform/services/nutrition/internal/handler"
	postgresrepo "fitness-platform/services/nutrition/internal/repository/postgres"
	"fitness-platform/services/nutrition/internal/service"
)

func main() {
	// 1. Инициализация логгера
	logger.Init("nutrition", "info")

	// 2. Загрузка конфигурации
	cfg, err := config.Load()
	if err != nil {
		logger.Log.Fatal().
			Err(err).
			Msg("Failed to load config")
	}

	// 3. Корневой контекст
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 4. Инициализация трассировки
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

	// 4. Подключение к базе данных
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

	// 6. Применяем миграции
	if err := runMigrations(cfg.DatabaseURL); err != nil {
		logger.Log.Fatal().
			Err(err).
			Msg("Failed to run migrations")
	}

	// 7. Создаём repository
	nutritionRepo := postgresrepo.NewNutritionRepo(
		pool,
		cfg.DatabaseOperationTimeout,
	)

	// 8. Создаём service
	nutritionService := service.NewNutritionService(
		nutritionRepo,
	)

	// 9. Создаём HTTP-handler
	nutritionHandler := handler.NewNutritionHandler(
		nutritionService,
	)

	// 7. Запускаем HTTP-сервер
	httpShutdown, err := server.RunREST(
		fmt.Sprintf(":%s", cfg.HTTPPort),
		nutritionHandler,
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
