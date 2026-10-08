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
	"fitness-platform/services/notification/internal/domain"
	"fitness-platform/services/notification/internal/worker"
)

// RedisConsumer читает события из Redis Streams и отправляет их в worker pool.
type RedisConsumer struct {
	redisClient *redis.Client
	stream      string
	group       string
	consumer    string
	pool        *worker.Pool

	claimMinIdle time.Duration
	claimCount   int64
	claimStart   string
}

// NewRedisConsumer создаёт нового consumer.
func NewRedisConsumer(
	redisClient *redis.Client,
	stream, group, consumerName string,
	pool *worker.Pool,
	claimMinIdle time.Duration,
	claimCount int64,
) *RedisConsumer {
	return &RedisConsumer{
		redisClient:  redisClient,
		stream:       stream,
		group:        group,
		consumer:     consumerName,
		pool:         pool,
		claimMinIdle: claimMinIdle,
		claimCount:   claimCount,
	}
}

// Run запускает цикл обработки сообщений.
func (c *RedisConsumer) Run(ctx context.Context) {
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
				Msg("notification consumer stopped")

			return

		default:
			c.recoverPendingBatch(ctx)
			c.processBatch(ctx)
			time.Sleep(1 * time.Second)
		}
	}
}

func (c *RedisConsumer) recoverPendingBatch(ctx context.Context) {
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
		if ctx.Err() == nil {
			logger.Log.Error().
				Err(err).
				Msg("failed to recover notification pending messages")
		}
		return
	}

	c.claimStart = nextStart

	if len(messages) == 0 {
		return
	}

	logger.Log.Info().
		Int("count", len(messages)).
		Msg("recovered notification pending messages")

	c.processMessages(ctx, messages)
}

// processBatch читает пачку сообщений и ставит notification tasks в worker pool.
func (c *RedisConsumer) processBatch(ctx context.Context) {
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

	if err != nil {
		if errors.Is(err, redis.Nil) || ctx.Err() != nil {
			return
		}

		logger.Log.Error().
			Err(err).
			Msg("failed to read notification redis stream")
		return
	}

	for _, stream := range streams {
		c.processMessages(ctx, stream.Messages)
	}
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

			task := domain.NotificationTask{
				UserID: workoutEvent.UserID,
				Type:   "workout_created",
				Message: fmt.Sprintf(
					"Новая тренировка '%s' записана!",
					workoutEvent.Name,
				),
			}

			if !c.pool.Submit(task) {
				logger.Log.Warn().
					Str("event_id", envelope.EventID).
					Str("redis_message_id", message.ID).
					Msg("notification worker pool rejected task")

				// Задачу не приняли — ACK не делаем.
				return
			}

			// Временно сохраняем ACK после Submit.
			// Исправим в notification-delivery PR.
			c.ackMessage(ctx, message.ID)

		case events.TypeWorkoutUpdated:
			logger.Log.Debug().
				Str("event_id", envelope.EventID).
				Msg("workout.updated is not handled by notification")

			c.ackMessage(ctx, message.ID)

		default:
			logger.Log.Debug().
				Str("event_id", envelope.EventID).
				Str("event_type", envelope.EventType).
				Msg("event type is not handled by notification")

			c.ackMessage(ctx, message.ID)
		}
	}
}

// parseEnvelope извлекает Envelope из Redis message.
func parseEnvelope(
	values map[string]interface{},
) (events.Envelope, error) {
	var envelope events.Envelope

	rawEvent, ok := values["event"].(string)
	if !ok {
		return envelope, errors.New("missing event")
	}

	if err := json.Unmarshal(
		[]byte(rawEvent),
		&envelope,
	); err != nil {
		return envelope, fmt.Errorf(
			"unmarshal event envelope: %w",
			err,
		)
	}

	return envelope, nil
}

// ackMessage подтверждает Redis Stream message.
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
