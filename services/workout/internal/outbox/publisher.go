package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"fitness-platform/pkg/events"
	"fitness-platform/pkg/logger"
	"fitness-platform/services/workout/internal/domain"
)

const (
	maxBackoffInterval = 60 * time.Second
	outboxClaimLease   = 30 * time.Second
	maxBatchSize       = 100
)

// Repository содержит только операции,
// необходимые Outbox Publisher.
type Repository interface {
	ClaimNextOutboxEvent(
		ctx context.Context,
		claimToken string,
		lease time.Duration,
	) (*domain.OutboxEvent, error)

	MarkOutboxEventPublishedClaimed(
		ctx context.Context,
		eventID string,
		claimToken string,
	) error
}

type Publisher struct {
	repo        Repository
	redisClient *redis.Client
	streamName  string
	interval    time.Duration
}

func NewPublisher(
	repo Repository,
	redisClient *redis.Client,
	streamName string,
	interval time.Duration,
) *Publisher {
	return &Publisher{
		repo:        repo,
		redisClient: redisClient,
		streamName:  streamName,
		interval:    interval,
	}
}

func (p *Publisher) Run(ctx context.Context) {
	currentInterval := p.interval

	timer := time.NewTimer(currentInterval)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Log.Info().
				Msg("outbox publisher stopped")
			return

		case <-timer.C:
			err := p.processBatch(ctx)

			if ctx.Err() != nil {
				return
			}

			if err != nil {
				currentInterval = nextBackoff(currentInterval)
				retryIn := withJitter(currentInterval)

				logger.Log.Error().
					Err(err).
					Dur("retry_in", retryIn).
					Msg("outbox batch processing failed")

				timer.Reset(retryIn)
				continue
			}

			currentInterval = p.interval
			timer.Reset(currentInterval)
		}
	}
}

// processBatch резервирует и публикует
// максимум maxBatchSize событий за один проход.
func (p *Publisher) processBatch(ctx context.Context) error {
	for i := 0; i < maxBatchSize; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		// Уникальный token для каждой попытки публикации.
		claimToken := uuid.NewString()

		event, err := p.repo.ClaimNextOutboxEvent(
			ctx,
			claimToken,
			outboxClaimLease,
		)
		if err != nil {
			return fmt.Errorf(
				"claim outbox event: %w",
				err,
			)
		}

		// Больше доступных событий нет.
		if event == nil {
			return nil
		}

		payloadJSON, err := json.Marshal(event.Payload)
		if err != nil {
			return fmt.Errorf(
				"marshal payload of event %s: %w",
				event.ID,
				err,
			)
		}

		envelope := events.Envelope{
			EventID:      event.ID,
			EventType:    event.EventType,
			EventVersion: event.EventVersion,
			OccurredAt:   event.CreatedAt,
			Payload:      json.RawMessage(payloadJSON),
		}

		envelopeJSON, err := json.Marshal(envelope)
		if err != nil {
			return fmt.Errorf(
				"marshal envelope of event %s: %w",
				event.ID,
				err,
			)
		}

		// Публикуем в Redis Streams.
		err = p.redisClient.XAdd(
			ctx,
			&redis.XAddArgs{
				Stream: p.streamName,
				Values: map[string]interface{}{
					"event": string(envelopeJSON),
				},
			},
		).Err()

		if err != nil {
			// Claim НЕ снимаем.
			// После окончания lease событие
			// снова станет доступным.
			return fmt.Errorf(
				"publish outbox event %s: %w",
				event.ID,
				err,
			)
		}

		// Помечаем опубликованным только
		// при совпадении claimToken.
		if err := p.repo.MarkOutboxEventPublishedClaimed(
			ctx,
			event.ID,
			claimToken,
		); err != nil {
			return fmt.Errorf(
				"mark outbox event %s published: %w",
				event.ID,
				err,
			)
		}
	}

	return nil
}

func nextBackoff(current time.Duration) time.Duration {
	if current >= maxBackoffInterval/2 {
		return maxBackoffInterval
	}

	return current * 2
}

func withJitter(interval time.Duration) time.Duration {
	maxJitter := interval / 5

	if maxJitter <= 0 {
		return interval
	}

	jitter := time.Duration(rand.Int64N(int64(maxJitter)))
	return interval - jitter
}
