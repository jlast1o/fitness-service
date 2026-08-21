package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"fitness-platform/pkg/logger"
	"fitness-platform/services/planner/internal/domain"
	"fitness-platform/services/planner/internal/service"
)

// RedisConsumer читает события из Redis Streams и обрабатывает их.
type RedisConsumer struct {
	redisClient *redis.Client
	stream      string
	group       string
	consumer    string
	planner     *service.PlannerService
}

// NewRedisConsumer создаёт нового потребителя.
func NewRedisConsumer(redisClient *redis.Client, stream, group, consumerName string, planner *service.PlannerService) *RedisConsumer {
	return &RedisConsumer{
		redisClient: redisClient,
		stream:      stream,
		group:       group,
		consumer:    consumerName,
		planner:     planner,
	}
}

// Run запускает цикл обработки.
func (c *RedisConsumer) Run(ctx context.Context) {
	err := c.redisClient.XGroupCreateMkStream(ctx, c.stream, c.group, "$").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		logger.Log.Error().Err(err).Msg("failed to create consumer group")
		return
	}

	for {
		select {
		case <-ctx.Done():
			logger.Log.Info().Msg("planner consumer stopped")
			return
		default:
			c.processBatch(ctx)
			time.Sleep(1 * time.Second)
		}
	}
}

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
				c.redisClient.XAck(ctx, c.stream, c.group, message.ID)
				continue
			}

			if err := c.planner.ProcessWorkoutCreated(ctx, event); err != nil {
				logger.Log.Error().Err(err).Str("message_id", message.ID).Msg("failed to process event")
				continue // не подтверждаем, попробуем позже
			}

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
