package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"fitness-platform/pkg/logger"
	"fitness-platform/services/analytics/internal/domain"
	"fitness-platform/services/analytics/internal/service"
)

// RedisConsumer читает события из Redis Streams и обрабатывает их.
type RedisConsumer struct {
	redisClient *redis.Client
	stream      string
	group       string
	consumer    string
	analytics   *service.AnalyticsService
}

// NewRedisConsumer создаёт нового потребителя.
func NewRedisConsumer(redisClient *redis.Client, stream, group, consumerName string, analytics *service.AnalyticsService) *RedisConsumer {
	return &RedisConsumer{
		redisClient: redisClient,
		stream:      stream,
		group:       group,
		consumer:    consumerName,
		analytics:   analytics,
	}
}

// Run запускает цикл обработки сообщений.
func (c *RedisConsumer) Run(ctx context.Context) {
	// Создаём группу потребителей, если её нет
	// "$" означает: начинаем читать только новые сообщения после создания группы
	err := c.redisClient.XGroupCreateMkStream(ctx, c.stream, c.group, "$").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		logger.Log.Error().Err(err).Msg("failed to create consumer group")
		return
	}

	for {
		select {
		case <-ctx.Done():
			logger.Log.Info().Msg("analytics consumer stopped")
			return
		default:
			c.processBatch(ctx)
			time.Sleep(1 * time.Second) // пауза между чтениями
		}
	}
}

// processBatch читает пачку сообщений и обрабатывает их.
func (c *RedisConsumer) processBatch(ctx context.Context) {
	// XReadGroup с Block: 0 — блокирующее чтение (ждёт новые сообщения)
	// Но так как у нас цикл с паузой, лучше поставить Block: 1 сек и без паузы.
	streams, err := c.redisClient.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    c.group,
		Consumer: c.consumer,
		Streams:  []string{c.stream, ">"},
		Count:    10,
		Block:    1 * time.Second,
	}).Result()
	if err != nil {
		if redis.HasErrorPrefix(err, "timeout") {
			// нет сообщений, просто выходим
			return
		}
		logger.Log.Error().Err(err).Msg("failed to read from redis stream")
		return
	}

	for _, stream := range streams {
		for _, message := range stream.Messages {
			event, err := parseEvent(message.Values)
			if err != nil {
				logger.Log.Error().Err(err).Str("message_id", message.ID).Msg("failed to parse event")
				// Подтверждаем плохое сообщение, чтобы не блокировать поток
				c.redisClient.XAck(ctx, c.stream, c.group, message.ID)
				continue
			}

			if err := c.analytics.ProcessWorkoutCreated(ctx, event); err != nil {
				logger.Log.Error().Err(err).Str("message_id", message.ID).Msg("failed to process event")
				// Не подтверждаем, чтобы обработать позже
				continue
			}

			// Успешно обработали — подтверждаем
			if err := c.redisClient.XAck(ctx, c.stream, c.group, message.ID).Err(); err != nil {
				logger.Log.Error().Err(err).Str("message_id", message.ID).Msg("failed to ack message")
			}
		}
	}
}

// parseEvent извлекает WorkoutCreatedEvent из полей сообщения.
func parseEvent(values map[string]interface{}) (domain.WorkoutCreatedEvent, error) {
	var event domain.WorkoutCreatedEvent
	payloadStr, ok := values["payload"].(string)
	if !ok {
		return event, errors.New("missing payload")
	}
	if err := json.Unmarshal([]byte(payloadStr), &event); err != nil {
		return event, err
	}
	return event, nil
}
