package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"fitness-platform/services/analytics/internal/domain"
	"fitness-platform/services/analytics/internal/repository"
	"fitness-platform/services/analytics/internal/repository/postgres"
)

// setupTestDB поднимает чистый PostgreSQL,
// применяет реальные migrations Analytics и возвращает pool.
func setupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	t.Setenv(
		"TESTCONTAINERS_HOST_OVERRIDE",
		"127.0.0.1",
	)

	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        "postgres:15-alpine",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "test",
			"POSTGRES_PASSWORD": "test",
			"POSTGRES_DB":       "testdb",
		},
		WaitingFor: wait.
			ForLog("database system is ready to accept connections").
			WithOccurrence(2).
			WithStartupTimeout(30 * time.Second),
	}

	postgresContainer, err := testcontainers.GenericContainer(
		ctx,
		testcontainers.GenericContainerRequest{
			ContainerRequest: req,
			Started:          true,
		},
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		if err := postgresContainer.Terminate(
			context.Background(),
		); err != nil {
			t.Logf(
				"failed to terminate postgres container: %v",
				err,
			)
		}
	})

	host, err := postgresContainer.Host(ctx)
	require.NoError(t, err)

	port, err := postgresContainer.MappedPort(
		ctx,
		"5432/tcp",
	)
	require.NoError(t, err)

	dbURL := fmt.Sprintf(
		"postgres://test:test@%s:%s/testdb?sslmode=disable",
		host,
		port.Port(),
	)

	m, err := migrate.New(
		"file://../../../migrations",
		dbURL,
	)
	require.NoError(t, err)

	require.NoError(t, m.Up())

	t.Cleanup(func() {
		sourceErr, databaseErr := m.Close()

		if sourceErr != nil {
			t.Logf(
				"failed to close migration source: %v",
				sourceErr,
			)
		}

		if databaseErr != nil {
			t.Logf(
				"failed to close migration database: %v",
				databaseErr,
			)
		}
	})

	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)

	require.NoError(t, pool.Ping(ctx))

	t.Cleanup(pool.Close)

	return pool
}

func TestWithEventTransaction_DuplicateEvent(t *testing.T) {
	pool := setupTestDB(t)

	repo := postgres.NewAnalyticsRepo(
		pool,
		5*time.Second,
	)

	const (
		eventID = "11111111-1111-1111-1111-111111111111"
		userID  = "22222222-2222-2222-2222-222222222222"
	)

	ctx := context.Background()

	callbackCalls := 0

	applied, err := repo.WithEventTransaction(
		ctx,
		eventID,
		func(
			ctx context.Context,
			tx repository.AnalyticsEventTx,
		) error {
			callbackCalls++

			return tx.UpsertUserStats(
				ctx,
				&domain.UserStats{
					UserID:        userID,
					TotalWorkouts: 1,
					TotalVolume:   100,
					AvgIntensity:  50,
				},
			)
		},
	)

	require.NoError(t, err)
	require.True(t, applied)
	require.Equal(t, 1, callbackCalls)

	// Повторно обрабатываем тот же event_id.
	applied, err = repo.WithEventTransaction(
		ctx,
		eventID,
		func(
			context.Context,
			repository.AnalyticsEventTx,
		) error {
			callbackCalls++

			return errors.New(
				"duplicate callback must not execute",
			)
		},
	)

	require.NoError(t, err)
	require.False(t, applied)

	// Callback второго события не должен был запуститься.
	require.Equal(t, 1, callbackCalls)

	var processedCount int

	err = pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM processed_events
			WHERE event_id = $1
		`,
		eventID,
	).Scan(&processedCount)

	require.NoError(t, err)
	require.Equal(t, 1, processedCount)

	var totalWorkouts int

	err = pool.QueryRow(
		ctx,
		`
			SELECT total_workouts
			FROM user_stats
			WHERE user_id = $1
		`,
		userID,
	).Scan(&totalWorkouts)

	require.NoError(t, err)

	// Главное доказательство:
	// повторное событие не увеличило статистику ещё раз.
	require.Equal(t, 1, totalWorkouts)
}

func TestWithEventTransaction_RollbackOnCallbackError(
	t *testing.T,
) {
	pool := setupTestDB(t)

	repo := postgres.NewAnalyticsRepo(
		pool,
		5*time.Second,
	)

	const (
		eventID = "33333333-3333-3333-3333-333333333333"
		userID  = "44444444-4444-4444-4444-444444444444"
	)

	ctx := context.Background()

	expectedErr := errors.New(
		"business processing failed",
	)

	applied, err := repo.WithEventTransaction(
		ctx,
		eventID,
		func(
			ctx context.Context,
			tx repository.AnalyticsEventTx,
		) error {
			err := tx.UpsertUserStats(
				ctx,
				&domain.UserStats{
					UserID:        userID,
					TotalWorkouts: 99,
					TotalVolume:   999,
					AvgIntensity:  99,
				},
			)
			if err != nil {
				return err
			}

			// Имитируем ошибку бизнес-обработки
			// после уже выполненного SQL-запроса.
			return expectedErr
		},
	)

	require.ErrorIs(t, err, expectedErr)
	require.False(t, applied)

	// event_id должен откатиться вместе
	// с бизнесовыми изменениями.
	var processedCount int

	err = pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM processed_events
			WHERE event_id = $1
		`,
		eventID,
	).Scan(&processedCount)

	require.NoError(t, err)
	require.Equal(t, 0, processedCount)

	// Изменения user_stats тоже должны откатиться.
	var statsCount int

	err = pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM user_stats
			WHERE user_id = $1
		`,
		userID,
	).Scan(&statsCount)

	require.NoError(t, err)
	require.Equal(t, 0, statsCount)

	// Теперь повторяем тот же event_id.
	// Так как первая транзакция откатилась,
	// событие должно считаться новым.
	applied, err = repo.WithEventTransaction(
		ctx,
		eventID,
		func(
			ctx context.Context,
			tx repository.AnalyticsEventTx,
		) error {
			return tx.UpsertUserStats(
				ctx,
				&domain.UserStats{
					UserID:        userID,
					TotalWorkouts: 1,
					TotalVolume:   100,
					AvgIntensity:  50,
				},
			)
		},
	)

	require.NoError(t, err)
	require.True(t, applied)

	var retryStatsCount int

	err = pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM user_stats
			WHERE user_id = $1
		`,
		userID,
	).Scan(&retryStatsCount)

	require.NoError(t, err)
	require.Equal(t, 1, retryStatsCount)
}

func TestWithEventTransaction_ConcurrentDuplicate(
	t *testing.T,
) {
	pool := setupTestDB(t)

	repo := postgres.NewAnalyticsRepo(
		pool,
		5*time.Second,
	)

	const eventID = "55555555-5555-5555-5555-555555555555"

	ctx := context.Background()

	var callbackCalls atomic.Int32
	var appliedCount atomic.Int32

	errCh := make(chan error, 2)

	var wg sync.WaitGroup
	wg.Add(2)

	run := func() {
		defer wg.Done()

		applied, err := repo.WithEventTransaction(
			ctx,
			eventID,
			func(
				context.Context,
				repository.AnalyticsEventTx,
			) error {
				callbackCalls.Add(1)

				// Первая транзакция некоторое время
				// удерживает claim event_id.
				time.Sleep(100 * time.Millisecond)

				return nil
			},
		)

		if err != nil {
			errCh <- err
			return
		}

		if applied {
			appliedCount.Add(1)
		}
	}

	go run()
	go run()

	wg.Wait()
	close(errCh)

	for err := range errCh {
		require.NoError(t, err)
	}

	require.Equal(
		t,
		int32(1),
		appliedCount.Load(),
	)

	require.Equal(
		t,
		int32(1),
		callbackCalls.Load(),
	)

	var processedCount int

	err := pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM processed_events
			WHERE event_id = $1
		`,
		eventID,
	).Scan(&processedCount)

	require.NoError(t, err)
	require.Equal(t, 1, processedCount)
}
