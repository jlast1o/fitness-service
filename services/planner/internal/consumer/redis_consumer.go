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
	planner *service.PlannerService,
	claimMinIdleTime time.Duration,
	claimCount int64,
) *RedisConsumer {
	return &RedisConsumer{
		redisClient:  redisClient,
		stream:       stream,
		group:        group,
		consumer:     consumerName,
		planner:      planner,
		claimMinIdle: claimMinIdleTime,
		claimCount:   claimCount,
		claimStart:   "0-0",
	}
}

// Run запускает цикл обработки.
func (c *RedisConsumer) Run(ctx context.Context) {
	err := c.redisClient.XGroupCreateMkStream(
		ctx, c.stream, c.group, "$",
	).Err()

	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		logger.Log.Error().
			Err(err).
			Msg("failed to create planner consumer group")
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
			logger.Log.Info().Msg("planner consumer stopped")
			return
		}

		if err := c.recoverPendingBatch(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}

			delay := retry.Next()

			logger.Log.Warn().
				Err(err).
				Dur("retry_in", delay).
				Msg("planner redis recovery failed")

			if backoff.Wait(ctx, delay) != nil {
				return
			}

			continue
		}

		if err := c.processBatch(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}

			delay := retry.Next()

			logger.Log.Warn().
				Err(err).
				Dur("retry_in", delay).
				Msg("planner redis stream read failed")

			if backoff.Wait(ctx, delay) != nil {
				return
			}

			continue
		}

		retry.Reset()

		if backoff.Wait(ctx, time.Second) != nil {
			return
		}
	}
}

// recoverPendingBatch проверяет и обрабатывает сообщения, которые были прочитаны, но не подтверждены.
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

	c.claimStart = nextStart

	if len(messages) > 0 {
		logger.Log.Info().
			Int("count", len(messages)).
			Msg("recovered planner pending messages")

		c.processMessages(ctx, messages)
	}

	return nil
}

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
		if ctx.Err() != nil {
			return
		}

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
					Msg("failed to unmarshal workout.created")

				c.ackMessage(ctx, message.ID)
				continue
			}

			if err := c.planner.ProcessWorkoutCreated(
				ctx,
				envelope.EventID,
				workoutEvent,
			); err != nil {
				logger.Log.Error().
					Err(err).
					Str("event_id", envelope.EventID).
					Str("redis_message_id", message.ID).
					Msg("failed to process workout.created")

				// Нет ACK: сообщение остаётся в PEL.
				continue
			}

			c.ackMessage(ctx, message.ID)

		case events.TypeWorkoutUpdated:
			logger.Log.Debug().
				Str("event_id", envelope.EventID).
				Msg("workout.updated is not handled by planner")

			c.ackMessage(ctx, message.ID)

		default:
			logger.Log.Debug().
				Str("event_id", envelope.EventID).
				Str("event_type", envelope.EventType).
				Msg("event type is not handled by planner")

			c.ackMessage(ctx, message.ID)
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

// ackMessage подтверждает сообщение в Redis consumer group.
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
