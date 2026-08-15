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
	"fitness-platform/services/workout/internal/database"
	"fitness-platform/services/workout/internal/handler"
	"fitness-platform/services/workout/internal/repository/postgres"
	"fitness-platform/services/workout/internal/server"
	"fitness-platform/services/workout/internal/service"
)

func main() {
	// 1. Инициализация логгера
	logger.Init("info")

	// 2. Загрузка конфигурации
	cfg, err := config.Load()
	if err != nil {
		logger.Log.Fatal().Err(err).Msg("Failed to load config")
	}

	// 3. Корневой контекст
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 4. Подключение к базе данных
	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Log.Fatal().Err(err).Msg("Failed to connect to database")
	}
	defer pool.Close()

	// 5. Применяем миграции (путь относительно рабочей директории)
	if err := runMigrations(cfg.DatabaseURL); err != nil {
		logger.Log.Fatal().Err(err).Msg("Failed to run migrations")
	}

	// 6. Создаём репозиторий
	workoutRepo := postgres.NewWorkoutRepo(pool)

	// 7. Создаём сервис
	workoutService := service.NewWorkoutService(workoutRepo)

	// 8. Создаём обработчики
	workoutHandler := handler.NewWorkoutHandler(workoutService)

	// 9. Запускаем HTTP-сервер с JWT
	httpShutdown, err := server.RunREST(fmt.Sprintf(":%s", cfg.HTTPPort), workoutHandler, cfg.JWTSecret)
	if err != nil {
		logger.Log.Fatal().Err(err).Msg("Failed to start HTTP server")
	}

	// 10. Graceful shutdown
	shutdown.Graceful(ctx, cancel, httpShutdown)
}

// runMigrations применяет SQL-миграции.
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
