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

			return tx.AccumulateUserStats(
				ctx,
				userID,
				100,
				2,
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

	// Callback duplicate-события не должен был запуститься.
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

	var (
		totalWorkouts int
		totalVolume   float64
		totalReps     int
		avgIntensity  float64
	)

	err = pool.QueryRow(
		ctx,
		`
			SELECT
				total_workouts,
				total_volume,
				total_reps,
				avg_intensity
			FROM user_stats
			WHERE user_id = $1
		`,
		userID,
	).Scan(
		&totalWorkouts,
		&totalVolume,
		&totalReps,
		&avgIntensity,
	)

	require.NoError(t, err)

	// Главное доказательство:
	// duplicate не применил delta второй раз.
	require.Equal(t, 1, totalWorkouts)
	require.InDelta(t, 100, totalVolume, 0.001)
	require.Equal(t, 2, totalReps)
	require.InDelta(t, 50, avgIntensity, 0.001)
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
			err := tx.AccumulateUserStats(
				ctx,
				userID,
				999,
				10,
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

	// user_stats тоже должен откатиться.
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

	// Повторяем тот же event_id.
	// Первая транзакция откатилась,
	// поэтому событие должно считаться новым.
	applied, err = repo.WithEventTransaction(
		ctx,
		eventID,
		func(
			ctx context.Context,
			tx repository.AnalyticsEventTx,
		) error {
			return tx.AccumulateUserStats(
				ctx,
				userID,
				100,
				2,
			)
		},
	)

	require.NoError(t, err)
	require.True(t, applied)

	var (
		totalWorkouts int
		totalVolume   float64
		totalReps     int
		avgIntensity  float64
	)

	err = pool.QueryRow(
		ctx,
		`
			SELECT
				total_workouts,
				total_volume,
				total_reps,
				avg_intensity
			FROM user_stats
			WHERE user_id = $1
		`,
		userID,
	).Scan(
		&totalWorkouts,
		&totalVolume,
		&totalReps,
		&avgIntensity,
	)

	require.NoError(t, err)
	require.Equal(t, 1, totalWorkouts)
	require.InDelta(t, 100, totalVolume, 0.001)
	require.Equal(t, 2, totalReps)
	require.InDelta(t, 50, avgIntensity, 0.001)
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

func TestWithEventTransaction_ConcurrentDifferentEventsAccumulateStats(
	t *testing.T,
) {
	pool := setupTestDB(t)

	repo := postgres.NewAnalyticsRepo(
		pool,
		5*time.Second,
	)

	const (
		eventID1 = "66666666-6666-6666-6666-666666666666"
		eventID2 = "77777777-7777-7777-7777-777777777777"
		userID   = "88888888-8888-8888-8888-888888888888"
	)

	ctx := context.Background()

	// Барьер нужен, чтобы обе транзакции сначала
	// получили свои разные event_id, а затем почти
	// одновременно попытались изменить одного user.
	ready := make(chan struct{}, 2)
	start := make(chan struct{})

	type result struct {
		applied bool
		err     error
	}

	resultCh := make(chan result, 2)

	var wg sync.WaitGroup
	wg.Add(2)

	process := func(
		eventID string,
		volume float64,
		reps int,
	) {
		defer wg.Done()

		applied, err := repo.WithEventTransaction(
			ctx,
			eventID,
			func(
				ctx context.Context,
				tx repository.AnalyticsEventTx,
			) error {
				ready <- struct{}{}

				// Обе callback-функции ждут,
				// пока main goroutine не отпустит их вместе.
				<-start

				return tx.AccumulateUserStats(
					ctx,
					userID,
					volume,
					reps,
				)
			},
		)

		resultCh <- result{
			applied: applied,
			err:     err,
		}
	}

	go process(
		eventID1,
		1000,
		20,
	)

	go process(
		eventID2,
		800,
		10,
	)

	// Ждём, пока обе транзакции дойдут
	// до точки непосредственно перед UPDATE.
	<-ready
	<-ready

	close(start)

	wg.Wait()
	close(resultCh)

	appliedCount := 0

	for result := range resultCh {
		require.NoError(t, result.err)

		if result.applied {
			appliedCount++
		}
	}

	// event_id разные, поэтому примениться должны оба.
	require.Equal(t, 2, appliedCount)

	var (
		totalWorkouts int
		totalVolume   float64
		totalReps     int
		avgIntensity  float64
	)

	err := pool.QueryRow(
		ctx,
		`
			SELECT
				total_workouts,
				total_volume,
				total_reps,
				avg_intensity
			FROM user_stats
			WHERE user_id = $1
		`,
		userID,
	).Scan(
		&totalWorkouts,
		&totalVolume,
		&totalReps,
		&avgIntensity,
	)

	require.NoError(t, err)

	// Event A:
	// +1 workout, +1000 volume, +20 reps
	//
	// Event B:
	// +1 workout, +800 volume, +10 reps
	//
	// Ожидаем сумму обеих delta,
	// независимо от порядка транзакций.
	require.Equal(t, 2, totalWorkouts)
	require.InDelta(t, 1800, totalVolume, 0.001)
	require.Equal(t, 30, totalReps)
	require.InDelta(t, 60, avgIntensity, 0.001)

	var processedCount int

	err = pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM processed_events
			WHERE event_id IN ($1, $2)
		`,
		eventID1,
		eventID2,
	).Scan(&processedCount)

	require.NoError(t, err)
	require.Equal(t, 2, processedCount)
}

func TestWithEventTransaction_ConcurrentDifferentEventsMergeExerciseProgress(
	t *testing.T,
) {
	pool := setupTestDB(t)

	repo := postgres.NewAnalyticsRepo(
		pool,
		5*time.Second,
	)

	const (
		eventID1   = "99999999-9999-9999-9999-999999999991"
		eventID2   = "99999999-9999-9999-9999-999999999992"
		userID     = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
		exerciseID = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	)

	ctx := context.Background()

	workoutDate1 := time.Date(
		2026,
		time.October,
		1,
		12,
		0,
		0,
		0,
		time.UTC,
	)

	workoutDate2 := time.Date(
		2026,
		time.October,
		6,
		12,
		0,
		0,
		0,
		time.UTC,
	)

	progress1 := domain.ExerciseProgress{
		UserID:        userID,
		ExerciseID:    exerciseID,
		BestWeight:    100,
		TotalReps:     30,
		LastWorkoutAt: workoutDate1,
		Estimated1RM:  120,
	}

	progress2 := domain.ExerciseProgress{
		UserID:        userID,
		ExerciseID:    exerciseID,
		BestWeight:    90,
		TotalReps:     20,
		LastWorkoutAt: workoutDate2,
		Estimated1RM:  105,
	}

	ready := make(chan struct{}, 2)
	start := make(chan struct{})

	type result struct {
		applied bool
		err     error
	}

	resultCh := make(chan result, 2)

	var wg sync.WaitGroup
	wg.Add(2)

	process := func(
		eventID string,
		progress domain.ExerciseProgress,
	) {
		defer wg.Done()

		applied, err := repo.WithEventTransaction(
			ctx,
			eventID,
			func(
				ctx context.Context,
				tx repository.AnalyticsEventTx,
			) error {
				ready <- struct{}{}
				<-start

				return tx.MergeExerciseProgress(
					ctx,
					&progress,
				)
			},
		)

		resultCh <- result{
			applied: applied,
			err:     err,
		}
	}

	go process(
		eventID1,
		progress1,
	)

	go process(
		eventID2,
		progress2,
	)

	// Обе разные транзакции готовы менять
	// одну строку exercise_progress.
	<-ready
	<-ready

	close(start)

	wg.Wait()
	close(resultCh)

	appliedCount := 0

	for result := range resultCh {
		require.NoError(t, result.err)

		if result.applied {
			appliedCount++
		}
	}

	require.Equal(t, 2, appliedCount)

	var (
		bestWeight    float64
		totalReps     int
		lastWorkoutAt time.Time
		estimated1RM  float64
	)

	err := pool.QueryRow(
		ctx,
		`
			SELECT
				best_weight,
				total_reps,
				last_workout_at,
				estimated_1rm
			FROM exercise_progress
			WHERE user_id = $1
			  AND exercise_id = $2
		`,
		userID,
		exerciseID,
	).Scan(
		&bestWeight,
		&totalReps,
		&lastWorkoutAt,
		&estimated1RM,
	)

	require.NoError(t, err)

	// Семантика merge:
	//
	// best_weight:
	// MAX(100, 90) = 100
	//
	// total_reps:
	// 30 + 20 = 50
	//
	// last_workout_at:
	// MAX(Oct 1, Oct 6) = Oct 6
	//
	// estimated_1rm:
	// MAX(120, 105) = 120
	require.InDelta(t, 100, bestWeight, 0.001)
	require.Equal(t, 50, totalReps)
	require.WithinDuration(
		t,
		workoutDate2,
		lastWorkoutAt,
		time.Microsecond,
	)
	require.InDelta(t, 120, estimated1RM, 0.001)

	var processedCount int

	err = pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM processed_events
			WHERE event_id IN ($1, $2)
		`,
		eventID1,
		eventID2,
	).Scan(&processedCount)

	require.NoError(t, err)
	require.Equal(t, 2, processedCount)
}
