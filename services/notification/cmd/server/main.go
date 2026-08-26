package main

import (
	"context"
	"os"
	"time"

	"github.com/redis/go-redis/v9"

	"fitness-platform/pkg/logger"
	"fitness-platform/pkg/shutdown"
	"fitness-platform/services/notification/internal/consumer"
	"fitness-platform/services/notification/internal/poller"
	"fitness-platform/services/notification/internal/sender"
	"fitness-platform/services/notification/internal/worker"
)

func main() {
	// 1. Инициализация логгера
	logger.Init("info")

	// 2. Читаем конфигурацию из переменных окружения
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	plannerBaseURL := os.Getenv("PLANNER_BASE_URL")
	if plannerBaseURL == "" {
		plannerBaseURL = "http://localhost:8083"
	}
	internalToken := os.Getenv("INTERNAL_TOKEN")
	if internalToken == "" {
		internalToken = "dev-secret-token" // для разработки
	}
	pollInterval := time.Hour // можно вынести в env, но для простоты фиксируем

	// 3. Корневой контекст
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 4. Подключение к Redis
	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
	if err := redisClient.Ping(ctx).Err(); err != nil {
		logger.Log.Fatal().Err(err).Msg("Failed to connect to Redis")
	}
	defer redisClient.Close()

	// 5. Создаём отправителя
	notificationSender := sender.NewLoggerSender()

	// 6. Создаём пул воркеров
	pool := worker.NewPool(ctx, notificationSender, 4, 100)
	pool.Start()

	// 7. Создаём и запускаем Redis Consumer
	redisConsumer := consumer.NewRedisConsumer(
		redisClient,
		"workout.events",
		"notification-group",
		"notification-consumer-1",
		pool,
	)
	go redisConsumer.Run(ctx)

	// 8. Создаём и запускаем Reminder Poller
	reminderPoller := poller.NewReminderPoller(
		plannerBaseURL,
		pool,
		pollInterval,
		internalToken,
	)
	go reminderPoller.Run(ctx)

	// 9. Graceful shutdown
	shutdown.Graceful(ctx, cancel,
		func(ctx context.Context) error {
			// Останавливаем пул: отменяем контекст, закрываем канал, ждём воркеров
			return pool.Shutdown(ctx)
		},
	)
}
