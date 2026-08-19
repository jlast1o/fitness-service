package outbox

import (
	"context"
	"encoding/json"
	"fitness-platform/pkg/logger"
	"fitness-platform/services/workout/internal/repository"
	"time"

	"github.com/redis/go-redis/v9"
)

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
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Log.Info().Msg("outbox publisher stopped")
			return
		case <-ticker.C:
			p.processBatch(ctx)
		}
	}
}

// processBatch выбирает неопубликованные события, отправляет их в Redis и помечает.
func (p *Publisher) processBatch(ctx context.Context) {
	events, err := p.repo.ListPendingOutboxEvents(ctx, 100)
	if err != nil {
		logger.Log.Error().Err(err).Msg("failed to list pending outbox events")
		return
	}

	for _, event := range events {
		// Сериализуем payload в JSON строку
		payloadJSON, err := json.Marshal(event.Payload)
		if err != nil {
			logger.Log.Error().Err(err).Str("event_id", event.ID).Msg("failed to marshal outbox payload")
			continue
		}

		// Отправляем в Redis Stream
		cmd := p.redisClient.XAdd(ctx, &redis.XAddArgs{
			Stream: p.streamName,
			Values: map[string]interface{}{
				"event_type": event.EventType,
				"payload":    string(payloadJSON),
			},
		})
		if err := cmd.Err(); err != nil {
			logger.Log.Error().Err(err).Str("event_id", event.ID).Msg("failed to publish event to redis")
			continue
		}

		// Помечаем как опубликованное
		if err := p.repo.MarkOutboxEventPublished(ctx, event.ID); err != nil {
			logger.Log.Error().Err(err).Str("event_id", event.ID).Msg("failed to mark outbox event published")
		}
	}
}
