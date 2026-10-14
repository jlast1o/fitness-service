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
	"fitness-platform/services/notification/internal/domain"
	"fitness-platform/services/notification/internal/sender"
)

const (
	notificationBatchSize   int64 = 1
	notificationSendTimeout       = 10 * time.Second
)

type RedisConsumer struct {
	redisClient *redis.Client
	stream      string
	group       string
	consumer    string
	sender      sender.Sender

	claimMinIdle time.Duration
	claimStart   string
}

func NewRedisConsumer(
	redisClient *redis.Client,
	stream, group, consumerName string,
	claimMinIdle time.Duration,
	notificationSender sender.Sender,
) *RedisConsumer {
	return &RedisConsumer{
		redisClient:  redisClient,
		stream:       stream,
		group:        group,
		consumer:     consumerName,
		sender:       notificationSender,
		claimMinIdle: claimMinIdle,
		claimStart:   "0-0",
	}
}

// Старый API для совместимости с существующими тестами.
func (c *RedisConsumer) Run(ctx context.Context) {
	c.RunWithContexts(ctx, ctx)
}

// pollCtx — получение новых сообщений.
// workCtx — выполнение Send и последующий XACK.
func (c *RedisConsumer) RunWithContexts(
	pollCtx context.Context,
	workCtx context.Context,
) {
	redisstartup.Runner{
		Client:  c.redisClient,
		Stream:  c.stream,
		Group:   c.group,
		Service: "notification",

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
			Count:    notificationBatchSize,
		},
	).Result()

	if err != nil {
		return fmt.Errorf("xautoclaim: %w", err)
	}

	if len(messages) > 0 {
		logger.Log.Info().
			Int("count", len(messages)).
			Msg("recovered notification pending messages")

		if err := c.processMessagesWithContexts(
			pollCtx,
			workCtx,
			messages,
		); err != nil {
			return err
		}
	}

	// Не меняем cursor, если обработка DLQ завершилась ошибкой.
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
			Count:    notificationBatchSize,
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

// Сохраняем прежние вызовы.
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
		// Не начинаем следующую работу после SIGTERM.
		if pollCtx.Err() != nil || workCtx.Err() != nil {
			return nil
		}

		envelope, err := parseEnvelope(message.Values)
		if err != nil {
			logger.Log.Error().
				Err(err).
				Str("redis_message_id", message.ID).
				Msg("failed to parse event envelope")

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
					Msg("failed to unmarshal workout.created")

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

			task := domain.NotificationTask{
				UserID: workoutEvent.UserID,
				Type:   "workout_created",
				Message: fmt.Sprintf(
					"Новая тренировка '%s' записана!",
					workoutEvent.Name,
				),
			}

			// Отправка уже начата, используем workCtx.
			sendCtx, cancel := context.WithTimeout(
				workCtx,
				notificationSendTimeout,
			)

			err := c.sender.Send(
				sendCtx,
				task.UserID,
				task.Message,
			)
			cancel()

			if err != nil {
				logger.Log.Error().
					Err(err).
					Str("event_id", envelope.EventID).
					Str("redis_message_id", message.ID).
					Msg(
						"failed to send notification; leaving message pending",
					)

				// Ошибка Sender — НЕ DLQ и НЕ ACK.
				// Оставляем событие для будущего recovery.
				return nil
			}

			// Сначала Send, затем XACK.
			c.ackMessage(workCtx, message.ID)

		case events.TypeWorkoutUpdated:
			logger.Log.Debug().
				Str("event_id", envelope.EventID).
				Msg("workout.updated is not handled by notification")

			c.ackMessage(workCtx, message.ID)

		default:
			// Неизвестный тип события не обязательно poison.
			logger.Log.Debug().
				Str("event_id", envelope.EventID).
				Str("event_type", envelope.EventType).
				Msg("event type is not handled by notification")

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
