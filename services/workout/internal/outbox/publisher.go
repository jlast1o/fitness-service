package outbox

import (
	"context"
	"encoding/json"
	"fitness-platform/pkg/events"
	"fitness-platform/pkg/logger"
	"fitness-platform/services/workout/internal/repository"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/redis/go-redis/v9"
)

const maxBackoffInterval = 60 * time.Second

// Publisher отвечает за публикацию outbox-событий в Redis Streams.
type Publisher struct {
	repo        repository.WorkoutRepository
	redisClient *redis.Client
	streamName  string
	interval    time.Duration
}

// NewPublisher создаёт новый экземпляр Publisher.
func NewPublisher(repo repository.WorkoutRepository, redisClient *redis.Client, streamName string, interval time.Duration) *Publisher {
	return &Publisher{
		repo:        repo,
		redisClient: redisClient,
		streamName:  streamName,
		interval:    interval,
	}
}

// Run запускает бесконечный цикл обработки outbox-событий.
// Останавливается по отмене контекста.
func (p *Publisher) Run(ctx context.Context) {
	currentInterval := p.interval

	timer := time.NewTimer(currentInterval)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Log.Info().Msg("outbox publisher stopped")
			return

		case <-timer.C:
			err := p.processBatch(ctx)

			if ctx.Err() != nil {
				logger.Log.Info().Msg("outbox publisher stopped")
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

// processBatch выбирает неопубликованные события, отправляет их в Redis и помечает.
func (p *Publisher) processBatch(ctx context.Context) error {
	pendingEvents, err := p.repo.ListPendingOutboxEvents(ctx, 100)
	if err != nil {
		return fmt.Errorf("list pending outbox events: %w", err)
	}

	for _, event := range pendingEvents {
		payloadJSON, err := json.Marshal(event.Payload)
		if err != nil {
			logger.Log.Error().Err(err).Str("event_id", event.ID).Msg("failed to marshal outbox payload")
			continue
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
			logger.Log.Error().Err(err).Str("event_id", event.ID).Msg("failed to marshal envelope")
			continue
		}

		if err := p.redisClient.XAdd(ctx, &redis.XAddArgs{
			Stream: p.streamName,
			Values: map[string]interface{}{
				"event": string(envelopeJSON),
			},
		}).Err(); err != nil {
			return fmt.Errorf("publish outbox event %s: %w", event.ID, err)
		}

		if err := p.repo.MarkOutboxEventPublished(ctx, event.ID); err != nil {
			return fmt.Errorf("mark outbox event %s as published: %w", event.ID, err)
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
	maxJitter := interval / 5 // до 20%

	if maxJitter <= 0 {
		return interval
	}

	jitter := time.Duration(rand.Int64N(int64(maxJitter)))

	return interval - jitter
}
