package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"fitness-platform/pkg/backoff"
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

	claimMinIdle time.Duration
	claimCount   int64
	claimStart   string
}

// NewRedisConsumer создаёт нового потребителя.
func NewRedisConsumer(
	redisClient *redis.Client,
	stream,
	group,
	consumerName string,
	analytics *service.AnalyticsService,
	claimMinIdle time.Duration,
	claimCount int64,
) *RedisConsumer {
	return &RedisConsumer{
		redisClient:  redisClient,
		stream:       stream,
		group:        group,
		consumer:     consumerName,
		analytics:    analytics,
		claimMinIdle: claimMinIdle,
		claimCount:   claimCount,
		claimStart:   "0-0",
	}
}

// Run запускает цикл обработки сообщений.
func (c *RedisConsumer) Run(ctx context.Context) {
	err := c.redisClient.XGroupCreateMkStream(
		ctx, c.stream, c.group, "$",
	).Err()

	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		logger.Log.Error().
			Err(err).
			Msg("failed to create analytics consumer group")
		return
	}

	retry, err := backoff.New(
		250*time.Millisecond,
		30*time.Second,
	)
	if err != nil {
		logger.Log.Error().
			Err(err).
			Msg("invalid redis consumer backoff settings")
		return
	}

	for {
		if ctx.Err() != nil {
			logger.Log.Info().Msg("analytics consumer stopped")
			return
		}

		// Сначала восстанавливаем pending-сообщения.
		if err := c.recoverPendingBatch(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}

			delay := retry.Next()

			logger.Log.Warn().
				Err(err).
				Dur("retry_in", delay).
				Msg("analytics redis recovery failed")

			if backoff.Wait(ctx, delay) != nil {
				return
			}

			continue
		}

		// Затем читаем новые сообщения.
		if err := c.processBatch(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}

			delay := retry.Next()

			logger.Log.Warn().
				Err(err).
				Dur("retry_in", delay).
				Msg("analytics redis stream read failed")

			if backoff.Wait(ctx, delay) != nil {
				return
			}

			continue
		}

		// Обе Redis-операции завершились успешно.
		retry.Reset()

		if backoff.Wait(ctx, time.Second) != nil {
			return
		}
	}
}

func (c *RedisConsumer) recoverPendingBatch(
	ctx context.Context,
) error {
	messages, nextStart, err := c.redisClient.XAutoClaim(
		ctx,
		&redis.XAutoClaimArgs{
			Stream:   c.stream,
			Group:    c.group,
			Consumer: c.consumer,
			MinIdle:  c.claimMinIdle,
			Start:    c.claimStart,
			Count:    c.claimCount,
		},
	).Result()

	if err != nil {
		return fmt.Errorf("xautoclaim: %w", err)
	}

	// Курсор меняем только после успешного XAUTOCLAIM.
	c.claimStart = nextStart

	if len(messages) > 0 {
		logger.Log.Info().
			Int("count", len(messages)).
			Msg("recovered analytics pending messages")

		c.processMessages(ctx, messages)
	}

	return nil
}

// processBatch читает пачку сообщений и обрабатывает их.
func (c *RedisConsumer) processBatch(
	ctx context.Context,
) error {
	streams, err := c.redisClient.XReadGroup(
		ctx,
		&redis.XReadGroupArgs{
			Group:    c.group,
			Consumer: c.consumer,
			Streams:  []string{c.stream, ">"},
			Count:    10,
			Block:    time.Second,
		},
	).Result()

	// Нет новых сообщений — нормальная ситуация.
	if errors.Is(err, redis.Nil) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("xreadgroup: %w", err)
	}

	for _, stream := range streams {
		c.processMessages(ctx, stream.Messages)
	}

	return nil
}

func (c *RedisConsumer) processMessages(
	ctx context.Context,
	messages []redis.XMessage,
) {
	for _, message := range messages {
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
				// После claimMinIdle сообщение снова
				// сможет быть поднято через XAUTOCLAIM.
				continue
			}

			c.ackMessage(
				ctx,
				message.ID,
			)

		case events.TypeWorkoutUpdated:
			logger.Log.Debug().
				Str("event_id", envelope.EventID).
				Msg("workout.updated is not handled by analytics")

			c.ackMessage(
				ctx,
				message.ID,
			)

		default:
			logger.Log.Debug().
				Str("event_id", envelope.EventID).
				Str("event_type", envelope.EventType).
				Msg("event type is not handled by analytics")

			c.ackMessage(
				ctx,
				message.ID,
			)
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
