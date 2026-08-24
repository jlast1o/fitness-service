package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"fitness-platform/pkg/logger"
	"fitness-platform/services/notification/internal/domain"
	"fitness-platform/services/notification/internal/worker"
)

// RedisConsumer читает события из Redis Streams и отправляет их в пул.
type RedisConsumer struct {
	redisClient *redis.Client
	stream      string
	group       string
	consumer    string
	pool        *worker.Pool
}

// NewRedisConsumer создаёт нового потребителя.
func NewRedisConsumer(redisClient *redis.Client, stream, group, consumerName string, pool *worker.Pool) *RedisConsumer {
	return &RedisConsumer{
		redisClient: redisClient,
		stream:      stream,
		group:       group,
		consumer:    consumerName,
		pool:        pool,
	}
}

// Run запускает цикл обработки.
func (c *RedisConsumer) Run(ctx context.Context) {
	// Создаём группу потребителей, если её нет
	err := c.redisClient.XGroupCreateMkStream(ctx, c.stream, c.group, "$").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		logger.Log.Error().Err(err).Msg("failed to create consumer group")
		return
	}

	for {
		select {
		case <-ctx.Done():
			logger.Log.Info().Msg("notification consumer stopped")
			return
		default:
			c.processBatch(ctx)
			time.Sleep(1 * time.Second) // пауза между чтениями
		}
	}
}

// processBatch читает пачку сообщений и ставит задачи в пул.
func (c *RedisConsumer) processBatch(ctx context.Context) {
	streams, err := c.redisClient.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    c.group,
		Consumer: c.consumer,
		Streams:  []string{c.stream, ">"},
		Count:    10,
		Block:    1 * time.Second,
	}).Result()
	if err != nil {
		if redis.HasErrorPrefix(err, "timeout") {
			return // нет новых сообщений
		}
		logger.Log.Error().Err(err).Msg("failed to read from redis stream")
		return
	}

	for _, stream := range streams {
		for _, message := range stream.Messages {
			event, err := parseWorkoutEvent(message.Values)
			if err != nil {
				logger.Log.Error().Err(err).Str("message_id", message.ID).Msg("failed to parse event")
				// Подтверждаем плохое сообщение, чтобы не застрять на нём
				c.redisClient.XAck(ctx, c.stream, c.group, message.ID)
				continue
			}

			// Формируем задачу для пула
			task := domain.NotificationTask{
				UserID:  event.UserID,
				Type:    "workout_created",
				Message: fmt.Sprintf("Новая тренировка '%s' записана!", event.Name),
			}

			// Пытаемся добавить задачу в пул
			if !c.pool.Submit(task) {
				// Пул остановлен — не подтверждаем сообщение, выходим
				return
			}

			// Подтверждаем, что задача принята (не обработана!)
			if err := c.redisClient.XAck(ctx, c.stream, c.group, message.ID).Err(); err != nil {
				logger.Log.Error().Err(err).Str("message_id", message.ID).Msg("failed to ack message")
			}
		}
	}
}

// parseWorkoutEvent извлекает WorkoutCreatedEvent из полей сообщения.
func parseWorkoutEvent(values map[string]interface{}) (*domain.WorkoutCreatedEvent, error) {
	payloadStr, ok := values["payload"].(string)
	if !ok {
		return nil, errors.New("missing payload")
	}
	var event domain.WorkoutCreatedEvent
	if err := json.Unmarshal([]byte(payloadStr), &event); err != nil {
		return nil, err
	}
	return &event, nil
}
