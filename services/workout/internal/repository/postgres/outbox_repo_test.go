package postgres_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"fitness-platform/pkg/events"
	"fitness-platform/pkg/testutil/redisstreamtest"
	"fitness-platform/services/workout/internal/outbox"
	"fitness-platform/services/workout/internal/repository/postgres"
)

const testClaimLease = 30 * time.Second

// Создаём неопубликованное Outbox-событие.
func insertOutboxForClaimTest(
	t *testing.T,
	pool *pgxpool.Pool,
) string {
	t.Helper()

	var id string

	err := pool.QueryRow(
		context.Background(),
		`
			INSERT INTO outbox_events (
				event_type,
				event_version,
				payload
			)
			VALUES (
				'workout.created',
				1,
				'{"workout_id":"test-workout"}'::jsonb
			)
			RETURNING id::text
		`,
	).Scan(&id)

	require.NoError(t, err)

	return id
}

// Два publisher одновременно пытаются забрать
// одно событие. Победитель должен быть только один.
func TestOutboxClaim_ConcurrentSingleEvent(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	id := insertOutboxForClaimTest(t, pool)

	repo := postgres.NewOutboxRepo(
		pool,
		5*time.Second,
	)

	type result struct {
		id  string
		err error
	}

	results := make(chan result, 2)
	start := make(chan struct{})

	var wg sync.WaitGroup

	for i := 0; i < 2; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			<-start

			event, err := repo.ClaimNextOutboxEvent(
				context.Background(),
				uuid.NewString(),
				testClaimLease,
			)

			r := result{err: err}

			if event != nil {
				r.id = event.ID
			}

			results <- r
		}()
	}

	close(start)
	wg.Wait()
	close(results)

	claimed := 0

	for r := range results {
		require.NoError(t, r.err)

		if r.id != "" {
			claimed++
			require.Equal(t, id, r.id)
		}
	}

	require.Equal(
		t,
		1,
		claimed,
		"only one publisher may claim the event",
	)
}

// Два publisher могут получить два разных события.
func TestOutboxClaim_ConcurrentDifferentEvents(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	id1 := insertOutboxForClaimTest(t, pool)
	id2 := insertOutboxForClaimTest(t, pool)

	repo := postgres.NewOutboxRepo(
		pool,
		5*time.Second,
	)

	type result struct {
		id    string
		token string
		err   error
	}

	results := make(chan result, 2)
	start := make(chan struct{})

	var wg sync.WaitGroup

	for i := 0; i < 2; i++ {
		token := uuid.NewString()

		wg.Add(1)

		go func(token string) {
			defer wg.Done()

			<-start

			event, err := repo.ClaimNextOutboxEvent(
				context.Background(),
				token,
				testClaimLease,
			)

			r := result{
				token: token,
				err:   err,
			}

			if event != nil {
				r.id = event.ID
			}

			results <- r
		}(token)
	}

	close(start)
	wg.Wait()
	close(results)

	claimed := make(map[string]string)

	for r := range results {
		require.NoError(t, r.err)
		require.NotEmpty(t, r.id)

		_, duplicate := claimed[r.id]

		require.False(
			t,
			duplicate,
			"one event was claimed twice",
		)

		claimed[r.id] = r.token
	}

	require.Len(t, claimed, 2)

	_, ok1 := claimed[id1]
	_, ok2 := claimed[id2]

	require.True(t, ok1 && ok2)

	// Проверяем, что ownership записан в PostgreSQL.
	for id, token := range claimed {
		var owner string

		err := pool.QueryRow(
			context.Background(),
			`
				SELECT claimed_by::text
				FROM outbox_events
				WHERE id = $1::uuid
			`,
			id,
		).Scan(&owner)

		require.NoError(t, err)
		require.Equal(t, token, owner)
	}
}

// Чужой token не должен позволять пометить
// событие опубликованным.
func TestOutboxClaim_OtherTokenCannotMarkPublished(
	t *testing.T,
) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	id := insertOutboxForClaimTest(t, pool)

	repo := postgres.NewOutboxRepo(
		pool,
		5*time.Second,
	)

	owner := uuid.NewString()
	stranger := uuid.NewString()

	event, err := repo.ClaimNextOutboxEvent(
		ctx,
		owner,
		testClaimLease,
	)

	require.NoError(t, err)
	require.NotNil(t, event)
	require.Equal(t, id, event.ID)

	// Другой publisher пытается подтвердить
	// событие с чужим token.
	err = repo.MarkOutboxEventPublishedClaimed(
		ctx,
		id,
		stranger,
	)

	require.Error(t, err)

	var stillOwnedAndUnpublished bool

	err = pool.QueryRow(
		ctx,
		`
			SELECT
				published_at IS NULL
				AND claimed_by = $2::uuid
			FROM outbox_events
			WHERE id = $1::uuid
		`,
		id,
		owner,
	).Scan(&stillOwnedAndUnpublished)

	require.NoError(t, err)
	require.True(t, stillOwnedAndUnpublished)

	// Настоящий владелец успешно подтверждает.
	require.NoError(
		t,
		repo.MarkOutboxEventPublishedClaimed(
			ctx,
			id,
			owner,
		),
	)

	var publishedAndReleased bool

	err = pool.QueryRow(
		ctx,
		`
			SELECT
				published_at IS NOT NULL
				AND claimed_by IS NULL
				AND claim_expires_at IS NULL
			FROM outbox_events
			WHERE id = $1::uuid
		`,
		id,
	).Scan(&publishedAndReleased)

	require.NoError(t, err)
	require.True(t, publishedAndReleased)

	// Повторное подтверждение уже невозможно.
	require.Error(
		t,
		repo.MarkOutboxEventPublishedClaimed(
			ctx,
			id,
			owner,
		),
	)

	// Опубликованное событие больше не выбирается.
	event, err = repo.ClaimNextOutboxEvent(
		ctx,
		uuid.NewString(),
		testClaimLease,
	)

	require.NoError(t, err)
	require.Nil(t, event)
}

// После истечения lease другой publisher может
// восстановить событие и получить новый claim token.
func TestOutboxClaim_ExpiredLeaseCanBeReclaimed(
	t *testing.T,
) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	id := insertOutboxForClaimTest(t, pool)

	repo := postgres.NewOutboxRepo(
		pool,
		5*time.Second,
	)

	firstToken := uuid.NewString()
	secondToken := uuid.NewString()

	firstEvent, err := repo.ClaimNextOutboxEvent(
		ctx,
		firstToken,
		testClaimLease,
	)

	require.NoError(t, err)
	require.NotNil(t, firstEvent)
	require.Equal(t, id, firstEvent.ID)

	// Пока lease действует, событие недоступно.
	notAvailable, err := repo.ClaimNextOutboxEvent(
		ctx,
		secondToken,
		testClaimLease,
	)

	require.NoError(t, err)
	require.Nil(t, notAvailable)

	// Имитируем истечение lease через PostgreSQL.
	// Так тест не должен ждать реальные 30 секунд.
	_, err = pool.Exec(
		ctx,
		`
			UPDATE outbox_events
			SET claim_expires_at =
				NOW() - INTERVAL '1 second'
			WHERE id = $1::uuid
		`,
		id,
	)

	require.NoError(t, err)

	secondEvent, err := repo.ClaimNextOutboxEvent(
		ctx,
		secondToken,
		testClaimLease,
	)

	require.NoError(t, err)
	require.NotNil(t, secondEvent)
	require.Equal(t, id, secondEvent.ID)

	// Старый token больше не имеет права
	// подтверждать событие.
	require.Error(
		t,
		repo.MarkOutboxEventPublishedClaimed(
			ctx,
			id,
			firstToken,
		),
	)

	// Новый владелец может подтвердить публикацию.
	require.NoError(
		t,
		repo.MarkOutboxEventPublishedClaimed(
			ctx,
			id,
			secondToken,
		),
	)
}

// Проверяем реальную работу SKIP LOCKED.
// Первая строка заблокирована другой транзакцией,
// поэтому Claim должен взять вторую.
func TestOutboxClaim_SkipsLockedRow(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	lockedID := insertOutboxForClaimTest(t, pool)
	availableID := insertOutboxForClaimTest(t, pool)

	// Гарантируем порядок выбора: lockedID первый.
	_, err := pool.Exec(
		ctx,
		`
			UPDATE outbox_events
			SET created_at = NOW() - INTERVAL '1 minute'
			WHERE id = $1::uuid
		`,
		lockedID,
	)

	require.NoError(t, err)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)

	defer func() {
		_ = tx.Rollback(context.Background())
	}()

	var selectedID string

	// Держим PostgreSQL row lock открытым.
	err = tx.QueryRow(
		ctx,
		`
			SELECT id::text
			FROM outbox_events
			WHERE id = $1::uuid
			FOR UPDATE
		`,
		lockedID,
	).Scan(&selectedID)

	require.NoError(t, err)
	require.Equal(t, lockedID, selectedID)

	claimCtx, cancel := context.WithTimeout(
		ctx,
		2*time.Second,
	)
	defer cancel()

	repo := postgres.NewOutboxRepo(
		pool,
		5*time.Second,
	)

	event, err := repo.ClaimNextOutboxEvent(
		claimCtx,
		uuid.NewString(),
		testClaimLease,
	)

	require.NoError(t, err)
	require.NotNil(t, event)

	// SKIP LOCKED пропускает первую строку
	// и позволяет забрать вторую.
	require.Equal(
		t,
		availableID,
		event.ID,
		"SKIP LOCKED must bypass the locked event",
	)
}

// Сквозная проверка:
// PostgreSQL Outbox → Publisher → Redis XADD
// → PostgreSQL published_at.
func TestOutboxPublisher_PublishesEventToRedis(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	redisClient := redisstreamtest.NewClient(t)

	ctx := context.Background()

	id := insertOutboxForClaimTest(t, pool)

	repo := postgres.NewOutboxRepo(
		pool,
		5*time.Second,
	)

	publisher := outbox.NewPublisher(
		repo,
		redisClient,
		"workout.events",
		20*time.Millisecond,
	)

	runCtx, cancel := context.WithCancel(ctx)

	done := make(chan struct{})

	go func() {
		defer close(done)
		publisher.Run(runCtx)
	}()

	defer func() {
		cancel()

		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("publisher did not stop after cancellation")
		}
	}()

	// Ждём публикации события и записи published_at.
	require.Eventually(
		t,
		func() bool {
			var published bool

			err := pool.QueryRow(
				ctx,
				`
					SELECT published_at IS NOT NULL
					FROM outbox_events
					WHERE id = $1::uuid
				`,
				id,
			).Scan(&published)

			if err != nil || !published {
				return false
			}

			length, err := redisClient.XLen(
				ctx,
				"workout.events",
			).Result()

			return err == nil && length == 1
		},
		8*time.Second,
		25*time.Millisecond,
	)

	// Проверяем, что Redis получил правильное событие.
	messages, err := redisClient.XRange(
		ctx,
		"workout.events",
		"-",
		"+",
	).Result()

	require.NoError(t, err)
	require.Len(t, messages, 1)

	raw, ok := messages[0].Values["event"].(string)
	require.True(t, ok)

	var envelope events.Envelope

	require.NoError(
		t,
		json.Unmarshal([]byte(raw), &envelope),
	)

	require.Equal(t, id, envelope.EventID)
	require.Equal(t, events.TypeWorkoutCreated, envelope.EventType)
	require.Equal(t, events.Version1, envelope.EventVersion)
}
