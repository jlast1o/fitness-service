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

	"fitness-platform/services/planner/internal/domain"
	"fitness-platform/services/planner/internal/repository"
	"fitness-platform/services/planner/internal/repository/postgres"
)

const (
	testUserID     = "11111111-1111-1111-1111-111111111111"
	testWeekID     = "22222222-2222-2222-2222-222222222222"
	testDayID      = "33333333-3333-3333-3333-333333333333"
	testPlannedID  = "44444444-4444-4444-4444-444444444444"
	testExerciseID = "ec9cc261-85b5-55e5-9d3b-598b0307d4d8"
)

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

func seedActivePlan(
	t *testing.T,
	repo repository.PlannerRepository,
	workoutDate time.Time,
) {
	t.Helper()

	plan := &domain.TrainingPlan{
		UserID:          testUserID,
		Name:            "Test plan",
		Goal:            "strength",
		ExperienceLevel: "intermediate",
		StartDate:       workoutDate,
		EndDate:         workoutDate.AddDate(0, 0, 30),
		Status:          "active",
		ProgressionRule: "rpe",
	}

	weeks := []domain.PlanWeek{
		{
			ID:         testWeekID,
			WeekNumber: 1,
			Focus:      "intensity",
		},
	}

	days := []domain.PlanDay{
		{
			ID:        testDayID,
			WeekID:    testWeekID,
			DayNumber: 1,
			Date:      workoutDate,
			Name:      "Test day",
		},
	}

	exercises := []domain.PlannedExercise{
		{
			ID:            testPlannedID,
			DayID:         testDayID,
			ExerciseID:    testExerciseID,
			TargetSets:    3,
			TargetRepsMin: 5,
			TargetRepsMax: 8,
			TargetWeight:  100,
			TargetRPE:     8,
			Notes:         "",
			OrderIndex:    1,
		},
	}

	err := repo.CreatePlan(
		context.Background(),
		plan,
		weeks,
		days,
		exercises,
	)
	require.NoError(t, err)
}

func getTargetWeight(
	t *testing.T,
	repo repository.PlannerRepository,
) float64 {
	t.Helper()

	exercises, err := repo.GetPlannedExercises(
		context.Background(),
		testDayID,
	)
	require.NoError(t, err)
	require.Len(t, exercises, 1)

	return exercises[0].TargetWeight
}

func TestWithEventTransaction_DuplicateEvent(t *testing.T) {
	pool := setupTestDB(t)

	repo := postgres.NewPlannerRepo(
		pool,
		5*time.Second,
	)

	workoutDate := time.Date(
		2026,
		time.October,
		4,
		0,
		0,
		0,
		0,
		time.UTC,
	)

	seedActivePlan(
		t,
		repo,
		workoutDate,
	)

	const eventID = "55555555-5555-5555-5555-555555555555"

	ctx := context.Background()

	callbackCalls := 0

	process := func(
		ctx context.Context,
		tx repository.PlannerEventTx,
	) error {
		callbackCalls++

		exercises, err := tx.GetPlannedExercisesForDate(
			ctx,
			testUserID,
			workoutDate,
		)
		if err != nil {
			return err
		}

		require.Len(t, exercises, 1)

		exercise := exercises[0]
		exercise.TargetWeight += 2.5

		return tx.UpdatePlannedExercise(
			ctx,
			&exercise,
		)
	}

	applied, err := repo.WithEventTransaction(
		ctx,
		eventID,
		process,
	)

	require.NoError(t, err)
	require.True(t, applied)

	applied, err = repo.WithEventTransaction(
		ctx,
		eventID,
		process,
	)

	require.NoError(t, err)
	require.False(t, applied)

	require.Equal(t, 1, callbackCalls)

	require.InDelta(
		t,
		102.5,
		getTargetWeight(t, repo),
		0.001,
	)

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
}

func TestWithEventTransaction_RollbackOnCallbackError(
	t *testing.T,
) {
	pool := setupTestDB(t)

	repo := postgres.NewPlannerRepo(
		pool,
		5*time.Second,
	)

	workoutDate := time.Date(
		2026,
		time.October,
		4,
		0,
		0,
		0,
		0,
		time.UTC,
	)

	seedActivePlan(
		t,
		repo,
		workoutDate,
	)

	const eventID = "66666666-6666-6666-6666-666666666666"

	ctx := context.Background()

	expectedErr := errors.New(
		"planner processing failed",
	)

	applied, err := repo.WithEventTransaction(
		ctx,
		eventID,
		func(
			ctx context.Context,
			tx repository.PlannerEventTx,
		) error {
			exercises, err := tx.GetPlannedExercisesForDate(
				ctx,
				testUserID,
				workoutDate,
			)
			if err != nil {
				return err
			}

			require.Len(t, exercises, 1)

			exercise := exercises[0]
			exercise.TargetWeight = 50

			if err := tx.UpdatePlannedExercise(
				ctx,
				&exercise,
			); err != nil {
				return err
			}

			// SQL уже был выполнен внутри tx,
			// но теперь имитируем ошибку бизнес-логики.
			return expectedErr
		},
	)

	require.ErrorIs(t, err, expectedErr)
	require.False(t, applied)

	// Изменение упражнения должно быть отменено.
	require.InDelta(
		t,
		100.0,
		getTargetWeight(t, repo),
		0.001,
	)

	// Claim event_id тоже должен откатиться.
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

	// Поэтому тот же event_id можно обработать заново.
	applied, err = repo.WithEventTransaction(
		ctx,
		eventID,
		func(
			ctx context.Context,
			tx repository.PlannerEventTx,
		) error {
			exercises, err := tx.GetPlannedExercisesForDate(
				ctx,
				testUserID,
				workoutDate,
			)
			if err != nil {
				return err
			}

			require.Len(t, exercises, 1)

			exercise := exercises[0]
			exercise.TargetWeight += 2.5

			return tx.UpdatePlannedExercise(
				ctx,
				&exercise,
			)
		},
	)

	require.NoError(t, err)
	require.True(t, applied)

	require.InDelta(
		t,
		102.5,
		getTargetWeight(t, repo),
		0.001,
	)
}

func TestWithEventTransaction_ConcurrentDuplicate(
	t *testing.T,
) {
	pool := setupTestDB(t)

	repo := postgres.NewPlannerRepo(
		pool,
		5*time.Second,
	)

	const eventID = "77777777-7777-7777-7777-777777777777"

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
				repository.PlannerEventTx,
			) error {
				callbackCalls.Add(1)

				// Удерживаем первую транзакцию,
				// чтобы второй goroutine тоже успел
				// попробовать claim того же event_id.
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
		callbackCalls.Load(),
	)

	require.Equal(
		t,
		int32(1),
		appliedCount.Load(),
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
