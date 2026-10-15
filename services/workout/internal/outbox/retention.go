package outbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"fitness-platform/pkg/logger"
)

const maxCleanupBatchesPerRun = 10

type CleanupRepository interface {
	DeletePublishedBefore(
		ctx context.Context,
		before time.Time,
		limit int,
	) (int64, error)
}

type Cleaner struct {
	repo      CleanupRepository
	retention time.Duration
	interval  time.Duration
	batchSize int
}

func NewCleaner(
	repo CleanupRepository,
	retention time.Duration,
	interval time.Duration,
	batchSize int,
) (*Cleaner, error) {
	if repo == nil {
		return nil, errors.New(
			"nil outbox cleanup repository",
		)
	}
	if retention <= 0 {
		return nil, errors.New(
			"outbox retention must be positive",
		)
	}
	if interval <= 0 {
		return nil, errors.New(
			"outbox cleanup interval must be positive",
		)
	}
	if batchSize <= 0 || batchSize > 1000 {
		return nil, errors.New(
			"outbox cleanup batch size must be between 1 and 1000",
		)
	}

	return &Cleaner{
		repo:      repo,
		retention: retention,
		interval:  interval,
		batchSize: batchSize,
	}, nil
}

// pollCtx останавливает новые batch.
// Уже начатый SQL-запрос завершается через workCtx.
func (c *Cleaner) RunWithContexts(
	pollCtx context.Context,
	workCtx context.Context,
) {
	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		select {
		case <-pollCtx.Done():
			return

		case <-timer.C:
			if pollCtx.Err() != nil {
				return
			}

			deleted, err := c.Cleanup(
				pollCtx,
				workCtx,
				time.Now().UTC(),
			)

			if err != nil {
				if pollCtx.Err() == nil &&
					workCtx.Err() == nil {
					logger.Log.Error().
						Err(err).
						Msg("outbox cleanup failed")
				}
			} else if deleted > 0 {
				logger.Log.Info().
					Int64("deleted", deleted).
					Msg("expired outbox events removed")
			}

			timer.Reset(c.interval)
		}
	}
}

// Выполняем не больше maxCleanupBatchesPerRun
// отдельных DELETE за один проход.
func (c *Cleaner) Cleanup(
	pollCtx context.Context,
	workCtx context.Context,
	now time.Time,
) (int64, error) {
	cutoff := now.Add(-c.retention)

	var total int64

	for i := 0; i < maxCleanupBatchesPerRun; i++ {
		if pollCtx.Err() != nil {
			return total, nil
		}

		if err := workCtx.Err(); err != nil {
			return total, err
		}

		count, err := c.repo.DeletePublishedBefore(
			workCtx,
			cutoff,
			c.batchSize,
		)
		if err != nil {
			return total, fmt.Errorf(
				"outbox cleanup batch: %w",
				err,
			)
		}

		total += count

		if count < int64(c.batchSize) {
			return total, nil
		}
	}

	return total, nil
}
