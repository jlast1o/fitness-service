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
	"fitness-platform/services/analytics/internal/consumer"
	"fitness-platform/services/analytics/internal/database"
	"fitness-platform/services/analytics/internal/handler"
	"fitness-platform/services/analytics/internal/repository/postgres"
	"fitness-platform/services/analytics/internal/server"
	"fitness-platform/services/analytics/internal/service"
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

	// 5. Применяем миграции
	if err := runMigrations(cfg.DatabaseURL); err != nil {
		logger.Log.Fatal().Err(err).Msg("Failed to run migrations")
	}

	// 6. Создаём репозиторий
	analyticsRepo := postgres.NewAnalyticsRepo(pool)

	// 7. Создаём сервис
	analyticsService := service.NewAnalyticsService(analyticsRepo)

	// 8. Создаём обработчики
	analyticsHandler := handler.NewAnalyticsHandler(analyticsService)

	// 9. Запускаем HTTP-сервер
	httpShutdown, err := server.RunREST(fmt.Sprintf(":%s", cfg.HTTPPort), analyticsHandler, cfg.JWTSecret)
	if err != nil {
		logger.Log.Fatal().Err(err).Msg("Failed to start HTTP server")
	}

	// 10. Подключаемся к Redis
	redisClient := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	if err := redisClient.Ping(ctx).Err(); err != nil {
		logger.Log.Fatal().Err(err).Msg("Failed to connect to Redis")
	}
	defer redisClient.Close()

	// 11. Создаём Consumer и запускаем в фоне
	redisConsumer := consumer.NewRedisConsumer(
		redisClient,
		"workout.events",       // поток
		"analytics-group",      // группа потребителей
		"analytics-consumer-1", // имя потребителя
		analyticsService,
	)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		redisConsumer.Run(ctx)
	}()

	// 12. Ожидаем сигнал завершения и корректно останавливаем всё
	shutdown.Graceful(ctx, cancel,
		httpShutdown,
		func(ctx context.Context) error {
			wg.Wait()
			return nil
		},
	)
}

// runMigrations применяет SQL-миграции из указанной директории.
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
