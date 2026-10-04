package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"fitness-platform/pkg/events"
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
func NewRedisConsumer(
	redisClient *redis.Client,
	stream,
	group,
	consumerName string,
	analytics *service.AnalyticsService,
) *RedisConsumer {
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
	// Создаём группу потребителей, если её нет.
	// "$" означает: начинаем читать только новые сообщения после создания группы.
	err := c.redisClient.XGroupCreateMkStream(
		ctx,
		c.stream,
		c.group,
		"$",
	).Err()

	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		logger.Log.Error().
			Err(err).
			Msg("failed to create consumer group")

		return
	}

	for {
		select {
		case <-ctx.Done():
			logger.Log.Info().
				Msg("analytics consumer stopped")

			return

		default:
			c.processBatch(ctx)
			time.Sleep(1 * time.Second)
		}
	}
}

// processBatch читает пачку сообщений и обрабатывает их.
func (c *RedisConsumer) processBatch(ctx context.Context) {
	streams, err := c.redisClient.XReadGroup(
		ctx,
		&redis.XReadGroupArgs{
			Group:    c.group,
			Consumer: c.consumer,
			Streams:  []string{c.stream, ">"},
			Count:    10,
			Block:    1 * time.Second,
		},
	).Result()

	if err != nil {
		if redis.HasErrorPrefix(err, "timeout") {
			return
		}

		logger.Log.Error().
			Err(err).
			Msg("failed to read from redis stream")

		return
	}

	for _, stream := range streams {
		for _, message := range stream.Messages {
			envelope, err := parseEnvelope(message.Values)
			if err != nil {
				logger.Log.Error().
					Err(err).
					Str("redis_message_id", message.ID).
					Msg("failed to parse event envelope")

				c.ackMessage(ctx, message.ID)
				continue
			}

			if envelope.EventVersion != events.Version1 {
				logger.Log.Error().
					Str("event_id", envelope.EventID).
					Str("event_type", envelope.EventType).
					Int("event_version", envelope.EventVersion).
					Msg("unsupported event version")

				c.ackMessage(ctx, message.ID)
				continue
			}

			switch envelope.EventType {
			case events.TypeWorkoutCreated:
				var workoutEvent domain.WorkoutCreatedEvent

				if err := json.Unmarshal(
					envelope.Payload,
					&workoutEvent,
				); err != nil {
					logger.Log.Error().
						Err(err).
						Str("event_id", envelope.EventID).
						Str("redis_message_id", message.ID).
						Msg("failed to unmarshal workout.created payload")

					c.ackMessage(ctx, message.ID)
					continue
				}

				if err := c.analytics.ProcessWorkoutCreated(
					ctx,
					envelope.EventID,
					workoutEvent,
				); err != nil {
					logger.Log.Error().
						Err(err).
						Str("event_id", envelope.EventID).
						Str("redis_message_id", message.ID).
						Msg("failed to process workout.created event")

					// ACK не делаем.
					// Сообщение остаётся pending для будущего recovery.
					continue
				}

				c.ackMessage(ctx, message.ID)

			case events.TypeWorkoutUpdated:
				// Analytics пока не умеет корректно пересчитывать
				// статистику при изменении уже существующей тренировки.
				logger.Log.Debug().
					Str("event_id", envelope.EventID).
					Msg("workout.updated is not handled by analytics")

				// Повторная доставка ничего не изменит,
				// поэтому не оставляем сообщение pending.
				c.ackMessage(ctx, message.ID)

			default:
				logger.Log.Debug().
					Str("event_id", envelope.EventID).
					Str("event_type", envelope.EventType).
					Msg("event type is not handled by analytics")

				c.ackMessage(ctx, message.ID)
			}
		}
	}
}

// parseEnvelope извлекает межсервисный Envelope из Redis message.
func parseEnvelope(values map[string]interface{}) (events.Envelope, error) {
	var envelope events.Envelope

	rawEvent, ok := values["event"].(string)
	if !ok {
		return envelope, errors.New("missing event")
	}

	if err := json.Unmarshal(
		[]byte(rawEvent),
		&envelope,
	); err != nil {
		return envelope, fmt.Errorf("unmarshal event envelope: %w", err)
	}

	return envelope, nil
}

// ackMessage подтверждает успешную или намеренно пропущенную обработку сообщения.
func (c *RedisConsumer) ackMessage(
	ctx context.Context,
	messageID string,
) {
	if err := c.redisClient.XAck(
		ctx,
		c.stream,
		c.group,
		messageID,
	).Err(); err != nil {
		logger.Log.Error().
			Err(err).
			Str("redis_message_id", messageID).
			Msg("failed to ack redis message")
	}
}
