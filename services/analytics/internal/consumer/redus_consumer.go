package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"fitness-platform/pkg/eventdlq"
	"fitness-platform/pkg/events"
	"fitness-platform/pkg/logger"
	"fitness-platform/pkg/redisstartup"
	"fitness-platform/services/analytics/internal/domain"
	"fitness-platform/services/analytics/internal/service"
)

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

// Старый API остаётся для совместимости.
func (c *RedisConsumer) Run(ctx context.Context) {
	c.RunWithContexts(ctx, ctx)
}

// pollCtx управляет получением новых сообщений.
// workCtx используется для уже начатой обработки.
func (c *RedisConsumer) RunWithContexts(
	pollCtx, workCtx context.Context,
) {
	redisstartup.Runner{
		Client:  c.redisClient,
		Stream:  c.stream,
		Group:   c.group,
		Service: "analytics",

		RecoverPending: func(ctx context.Context) error {
			return c.recoverPendingWithContexts(
				ctx,
				workCtx,
			)
		},

		ReadNew: func(ctx context.Context) error {
			return c.readBatchWithContexts(
				ctx,
				workCtx,
			)
		},

		OnGroupReady: func() {
			c.claimStart = "0-0"
		},
	}.Run(pollCtx)
}

// Совместимость с существующими тестами.
func (c *RedisConsumer) recoverPendingBatch(
	ctx context.Context,
) error {
	return c.recoverPendingWithContexts(ctx, ctx)
}

func (c *RedisConsumer) recoverPendingWithContexts(
	pollCtx, workCtx context.Context,
) error {
	messages, nextStart, err := c.redisClient.XAutoClaim(
		pollCtx,
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

	if len(messages) > 0 {
		logger.Log.Info().
			Int("count", len(messages)).
			Msg("recovered analytics pending messages")

		if err := c.processMessagesWithContexts(
			pollCtx,
			workCtx,
			messages,
		); err != nil {
			return err
		}
	}

	// При ошибке DLQ не продвигаем cursor.
	c.claimStart = nextStart
	return nil
}

// Совместимость со старым API.
func (c *RedisConsumer) processBatch(
	ctx context.Context,
) error {
	return c.readBatchWithContexts(ctx, ctx)
}

func (c *RedisConsumer) readBatchWithContexts(
	pollCtx, workCtx context.Context,
) error {
	streams, err := c.redisClient.XReadGroup(
		pollCtx,
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
		if err := c.processMessagesWithContexts(
			pollCtx,
			workCtx,
			stream.Messages,
		); err != nil {
			return err
		}
	}

	return nil
}

// Старый метод сохраняем для тестов и вызовов.
func (c *RedisConsumer) processMessages(
	ctx context.Context,
	messages []redis.XMessage,
) {
	if err := c.processMessagesWithContexts(
		ctx,
		ctx,
		messages,
	); err != nil {
		logger.Log.Error().
			Err(err).
			Msg("redis consumer batch failed")
	}
}

func (c *RedisConsumer) processMessagesWithContexts(
	pollCtx, workCtx context.Context,
	messages []redis.XMessage,
) error {
	for _, message := range messages {
		// После SIGTERM не начинаем следующую обработку.
		if pollCtx.Err() != nil || workCtx.Err() != nil {
			return nil
		}

		envelope, err := parseEnvelope(message.Values)
		if err != nil {
			logger.Log.Error().
				Err(err).
				Str("redis_message_id", message.ID).
				Msg("failed to parse event envelope")

			// Сначала DLQ, потом ACK оригинала.
			if dlqErr := eventdlq.PublishAndAck(
				workCtx,
				c.redisClient,
				c.stream,
				c.group,
				message,
				eventdlq.InvalidEnvelope,
				err.Error(),
			); dlqErr != nil {
				return dlqErr
			}

			continue
		}

		if envelope.EventVersion != events.Version1 {
			logger.Log.Error().
				Str("event_id", envelope.EventID).
				Str("event_type", envelope.EventType).
				Int("event_version", envelope.EventVersion).
				Msg("unsupported event version")

			if dlqErr := eventdlq.PublishAndAck(
				workCtx,
				c.redisClient,
				c.stream,
				c.group,
				message,
				eventdlq.UnsupportedVersion,
				fmt.Sprintf(
					"event version %d",
					envelope.EventVersion,
				),
			); dlqErr != nil {
				return dlqErr
			}

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

				if dlqErr := eventdlq.PublishAndAck(
					workCtx,
					c.redisClient,
					c.stream,
					c.group,
					message,
					eventdlq.InvalidPayload,
					err.Error(),
				); dlqErr != nil {
					return dlqErr
				}

				continue
			}

			// Ошибки бизнес-логики НЕ отправляем в DLQ.
			if err := c.analytics.ProcessWorkoutCreated(
				workCtx,
				envelope.EventID,
				workoutEvent,
			); err != nil {
				logger.Log.Error().
					Err(err).
					Str("event_id", envelope.EventID).
					Str("redis_message_id", message.ID).
					Msg("failed to process workout.created event")

				// Нет ACK: событие остаётся pending.
				continue
			}

			// Успешная бизнес-обработка.
			c.ackMessage(workCtx, message.ID)

		case events.TypeWorkoutUpdated:
			logger.Log.Debug().
				Str("event_id", envelope.EventID).
				Msg("workout.updated is not handled by analytics")

			c.ackMessage(workCtx, message.ID)

		default:
			// Корректный, но неизвестный тип не обязан
			// обрабатываться этим consumer.
			logger.Log.Debug().
				Str("event_id", envelope.EventID).
				Str("event_type", envelope.EventType).
				Msg("event type is not handled by analytics")

			c.ackMessage(workCtx, message.ID)
		}
	}

	return nil
}

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

	// JSON мог быть синтаксически корректным,
	// но не содержать обязательные поля.
	if envelope.EventID == "" ||
		envelope.EventType == "" ||
		len(envelope.Payload) == 0 ||
		string(envelope.Payload) == "null" {
		return envelope, errors.New(
			"incomplete event envelope",
		)
	}

	return envelope, nil
}

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
