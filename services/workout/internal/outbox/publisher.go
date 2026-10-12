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

// Run сохраняет совместимость со старым API.
func (p *Publisher) Run(ctx context.Context) {
	p.RunWithContexts(ctx, ctx)
}

// RunWithContexts разделяет получение новой работы
// и завершение уже захваченного события.
func (p *Publisher) RunWithContexts(
	pollCtx context.Context,
	workCtx context.Context,
) {
	currentInterval := p.interval

	timer := time.NewTimer(currentInterval)
	defer timer.Stop()

	for {
		select {
		case <-pollCtx.Done():
			logger.Log.Info().
				Msg("outbox publisher stopped")
			return

		case <-timer.C:
			if pollCtx.Err() != nil {
				return
			}

			err := p.processBatchWithContexts(
				pollCtx,
				workCtx,
			)

			if pollCtx.Err() != nil {
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
// Сохраняем старый API для существующих вызовов.
func (p *Publisher) processBatch(
	ctx context.Context,
) error {
	return p.processBatchWithContexts(ctx, ctx)
}

func (p *Publisher) processBatchWithContexts(
	pollCtx context.Context,
	workCtx context.Context,
) error {
	for i := 0; i < maxBatchSize; i++ {
		// Не начинаем захват следующего события
		// после начала shutdown.
		if err := pollCtx.Err(); err != nil {
			return err
		}

		if err := workCtx.Err(); err != nil {
			return err
		}

		claimToken := uuid.NewString()

		// Захват нового события относится к polling.
		event, err := p.repo.ClaimNextOutboxEvent(
			pollCtx,
			claimToken,
			outboxClaimLease,
		)
		if err != nil {
			return fmt.Errorf(
				"claim outbox event: %w",
				err,
			)
		}

		if event == nil {
			return nil
		}

		// С этого момента у нас уже есть захваченное
		// событие. Его завершаем через workCtx.
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

		// Уже захваченную запись пытаемся
		// опубликовать до shutdown deadline.
		err = p.redisClient.XAdd(
			workCtx,
			&redis.XAddArgs{
				Stream: p.streamName,
				Values: map[string]interface{}{
					"event": string(envelopeJSON),
				},
			},
		).Err()

		if err != nil {
			// Lease останется до истечения срока.
			// Затем событие можно захватить повторно.
			return fmt.Errorf(
				"publish outbox event %s: %w",
				event.ID,
				err,
			)
		}

		// После XADD обязательно пытаемся сохранить
		// результат публикации в PostgreSQL.
		// Claim token защищает от устаревшего владельца.
		if err := p.repo.MarkOutboxEventPublishedClaimed(
			workCtx,
			event.ID,
			claimToken,
		); err != nil {
			return fmt.Errorf(
				"mark outbox event %s published: %w",
				event.ID,
				err,
			)
		}

		// Перед следующей итерацией снова
		// проверим pollCtx.
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
