package main

import (
	"context"
	"os"
	"time"

	"github.com/kelseyhightower/envconfig"
	"github.com/redis/go-redis/v9"

	"fitness-platform/pkg/logger"
	"fitness-platform/pkg/shutdown"
	"fitness-platform/services/notification/internal/consumer"
	"fitness-platform/services/notification/internal/poller"
	"fitness-platform/services/notification/internal/sender"
	"fitness-platform/services/notification/internal/worker"
)

type consumerRecoveryConfig struct {
	ClaimMinIdle time.Duration `envconfig:"REDIS_CONSUMER_CLAIM_MIN_IDLE" default:"30s"`
}

func main() {
	// 1. Инициализация логгера
	logger.Init("notification", "info")

	var recoveryCfg consumerRecoveryConfig

	if err := envconfig.Process("", &recoveryCfg); err != nil {
		logger.Log.Fatal().
			Err(err).
			Msg("failed to load consumer recovery config")
	}

	if recoveryCfg.ClaimMinIdle < 20*time.Second {
		logger.Log.Fatal().
			Msg("notification claim min idle must be at least 20s")
	}

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

	pollCtx, stopPolling := context.WithCancel(ctx)
	defer stopPolling()

	workCtx, cancelWork := context.WithCancel(ctx)
	defer cancelWork()
	// 4. Подключение к Redis
	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})

	defer redisClient.Close()

	// 5. Создаём отправителя
	notificationSender := sender.NewLoggerSender()

	// 6. Создаём пул воркеров
	pool := worker.NewPool(
		workCtx,
		notificationSender,
		4,
		100,
	)
	pool.Start()

	// 7. Создаём и запускаем Redis Consumer
	redisConsumer := consumer.NewRedisConsumer(
		redisClient,
		"workout.events",
		"notification-group",
		"notification-consumer-1",
		recoveryCfg.ClaimMinIdle,
		notificationSender,
	)

	consumerDone := make(chan struct{})

	go func() {
		defer close(consumerDone)

		redisConsumer.RunWithContexts(
			pollCtx,
			workCtx,
		)
	}()

	// 8. Создаём и запускаем Reminder Poller
	reminderPoller := poller.NewReminderPoller(
		plannerBaseURL,
		pool,
		pollInterval,
		internalToken,
	)
	reminderDone := make(chan struct{})

	go func() {
		defer close(reminderDone)

		reminderPoller.Run(pollCtx)
	}()

	// Объединяем завершение трёх фоновых компонентов
	// в один сигнал для GracefulDrain.
	backgroundDone := make(chan struct{})

	go func() {
		defer close(backgroundDone)

		// 1. Consumer перестал принимать события.
		<-consumerDone

		// 2. Reminder Poller перестал создавать задачи.
		<-reminderDone

		// 3. Теперь безопасно опустошаем Worker Pool.
		if err := pool.Shutdown(workCtx); err != nil {
			logger.Log.Error().
				Err(err).
				Msg("notification worker pool shutdown incomplete")
		}
	}()

	if err := shutdown.GracefulDrain(
		stopPolling,
		cancelWork,
		backgroundDone,
		nil, // У Notification нет HTTP-сервера.
		nil, // Нет tracing shutdown.
	); err != nil {
		logger.Log.Error().
			Err(err).
			Msg("incomplete notification shutdown")
	}
}
