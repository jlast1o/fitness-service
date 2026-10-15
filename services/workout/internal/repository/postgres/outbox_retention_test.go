package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"fitness-platform/services/workout/internal/repository/postgres"
)

func insertOutboxRetentionEvent(
	t *testing.T,
	pool *pgxpool.Pool,
	publishedAt *time.Time,
) string {
	t.Helper()

	var id string

	err := pool.QueryRow(
		context.Background(),
		`
		INSERT INTO outbox_events (
			event_type, event_version, payload, published_at
		)
		VALUES ('workout.created', 1, '{}'::jsonb, $1)
		RETURNING id::text
		`,
		publishedAt,
	).Scan(&id)

	require.NoError(t, err)
	return id
}

func TestOutboxRetention_DeletesOnlyExpiredPublishedInBatches(
	t *testing.T,
) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	repo := postgres.NewOutboxRepo(pool, 5*time.Second)

	now := time.Now().UTC().Truncate(time.Microsecond)
	cutoff := now.Add(-30 * 24 * time.Hour)

	old := cutoff.Add(-24 * time.Hour)
	fresh := cutoff.Add(time.Hour)

	old1 := insertOutboxRetentionEvent(t, pool, &old)
	old2 := insertOutboxRetentionEvent(t, pool, &old)
	freshID := insertOutboxRetentionEvent(t, pool, &fresh)
	boundaryID := insertOutboxRetentionEvent(t, pool, &cutoff)
	unpublishedID := insertOutboxRetentionEvent(t, pool, nil)

	// Удаляем по одной записи.
	for i := 0; i < 2; i++ {
		count, err := repo.DeletePublishedBefore(
			ctx, cutoff, 1,
		)

		require.NoError(t, err)
		require.EqualValues(t, 1, count)
	}

	// Старых опубликованных больше не осталось.
	count, err := repo.DeletePublishedBefore(
		ctx, cutoff, 1,
	)
	require.NoError(t, err)
	require.Zero(t, count)

	for _, id := range []string{old1, old2} {
		var exists bool

		err := pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM outbox_events
				WHERE id = $1::uuid
			)
		`, id).Scan(&exists)

		require.NoError(t, err)
		require.False(t, exists)
	}

	// Свежие, граничные и неопубликованные сохранены.
	for _, id := range []string{
		freshID, boundaryID, unpublishedID,
	} {
		var exists bool

		err := pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM outbox_events
				WHERE id = $1::uuid
			)
		`, id).Scan(&exists)

		require.NoError(t, err)
		require.True(t, exists)
	}

	// Publisher всё ещё может захватить
	// неопубликованное событие.
	claimed, err := repo.ClaimNextOutboxEvent(
		ctx,
		uuid.NewString(),
		30*time.Second,
	)

	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.Equal(t, unpublishedID, claimed.ID)
}

func TestOutboxRetention_SkipsLockedPublishedRows(
	t *testing.T,
) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	repo := postgres.NewOutboxRepo(pool, 2*time.Second)

	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	older := cutoff.Add(-48 * time.Hour)
	newer := cutoff.Add(-24 * time.Hour)

	lockedID := insertOutboxRetentionEvent(t, pool, &older)
	freeID := insertOutboxRetentionEvent(t, pool, &newer)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)

	defer func() {
		_ = tx.Rollback(context.Background())
	}()

	var id string

	err = tx.QueryRow(ctx, `
		SELECT id::text
		FROM outbox_events
		WHERE id = $1::uuid
		FOR UPDATE
	`, lockedID).Scan(&id)

	require.NoError(t, err)

	timeoutCtx, cancel := context.WithTimeout(
		ctx, 2*time.Second,
	)
	defer cancel()

	deleted, err := repo.DeletePublishedBefore(
		timeoutCtx, cutoff, 1,
	)

	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)

	var lockedExists, freeExists bool

	require.NoError(t, pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM outbox_events
			WHERE id = $1::uuid
		)
	`, lockedID).Scan(&lockedExists))

	require.NoError(t, pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM outbox_events
			WHERE id = $1::uuid
		)
	`, freeID).Scan(&freeExists))

	require.True(t, lockedExists)
	require.False(t, freeExists)
}
