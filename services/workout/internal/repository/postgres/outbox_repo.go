package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fitness-platform/services/workout/internal/domain"
)

type OutboxRepo struct {
	pool             *pgxpool.Pool
	operationTimeout time.Duration
}

func NewOutboxRepo(
	pool *pgxpool.Pool,
	operationTimeout time.Duration,
) *OutboxRepo {
	return &OutboxRepo{
		pool:             pool,
		operationTimeout: operationTimeout,
	}
}

// ClaimNextOutboxEvent атомарно резервирует одно событие.
// Разные publishers не должны получить одну и ту же
// запись, пока её lease действительно.
func (r *OutboxRepo) ClaimNextOutboxEvent(
	ctx context.Context,
	claimToken string,
	lease time.Duration,
) (*domain.OutboxEvent, error) {
	if lease <= 0 {
		return nil, errors.New("outbox lease must be positive")
	}

	if r.operationTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(
			ctx,
			r.operationTimeout,
		)
		defer cancel()
	}

	const query = `
		WITH candidate AS (
			SELECT id
			FROM outbox_events
			WHERE published_at IS NULL
			  AND (
				  claim_expires_at IS NULL
				  OR claim_expires_at <= NOW()
			  )
			ORDER BY created_at, id
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		UPDATE outbox_events AS o
		SET
			claimed_by = $1::uuid,
			claim_expires_at =
				NOW() + ($2::double precision * INTERVAL '1 second')
		FROM candidate
		WHERE o.id = candidate.id
		RETURNING
			o.id,
			o.event_type,
			o.event_version,
			o.payload,
			o.created_at,
			o.published_at
	`

	var event domain.OutboxEvent

	err := r.pool.QueryRow(
		ctx,
		query,
		claimToken,
		lease.Seconds(),
	).Scan(
		&event.ID,
		&event.EventType,
		&event.EventVersion,
		&event.Payload,
		&event.CreatedAt,
		&event.PublishedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf(
			"claim next outbox event: %w",
			err,
		)
	}

	return &event, nil
}

// MarkOutboxEventPublishedClaimed подтверждает публикацию
// только для владельца актуального claimToken.
func (r *OutboxRepo) MarkOutboxEventPublishedClaimed(
	ctx context.Context,
	eventID string,
	claimToken string,
) error {
	if r.operationTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(
			ctx,
			r.operationTimeout,
		)
		defer cancel()
	}

	const query = `
		UPDATE outbox_events
		SET
			published_at = NOW(),
			claimed_by = NULL,
			claim_expires_at = NULL
		WHERE id = $1::uuid
		  AND claimed_by = $2::uuid
		  AND published_at IS NULL
	`

	result, err := r.pool.Exec(
		ctx,
		query,
		eventID,
		claimToken,
	)
	if err != nil {
		return fmt.Errorf(
			"mark claimed outbox event published: %w",
			err,
		)
	}

	if result.RowsAffected() != 1 {
		return fmt.Errorf(
			"outbox event %s: claim lost or already published",
			eventID,
		)
	}

	return nil
}
