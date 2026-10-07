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

func seedPlannerUser(
	t *testing.T,
	repo repository.PlannerRepository,
	userID string,
) {
	t.Helper()

	err := repo.UpsertUserProfile(
		context.Background(),
		&domain.UserProfile{
			UserID:          userID,
			Goal:            "strength",
			ExperienceLevel: "intermediate",
			DaysPerWeek:     3,
			Injuries:        map[string]any{},
			Current1RM:      map[string]float64{},
		},
	)

	require.NoError(t, err)
}

func makePlanGraph(
	userID string,
	name string,
	startDate time.Time,
	weekID string,
	dayID string,
	plannedExerciseID string,
) (
	*domain.TrainingPlan,
	[]domain.PlanWeek,
	[]domain.PlanDay,
	[]domain.PlannedExercise,
) {
	plan := &domain.TrainingPlan{
		UserID:          userID,
		Name:            name,
		Goal:            "strength",
		ExperienceLevel: "intermediate",
		StartDate:       startDate,
		EndDate:         startDate.AddDate(0, 1, 0),
		Status:          "active",
		ProgressionRule: "linear",
	}

	weeks := []domain.PlanWeek{
		{
			ID:         weekID,
			WeekNumber: 1,
			Focus:      "volume",
		},
	}

	days := []domain.PlanDay{
		{
			ID:        dayID,
			WeekID:    weekID,
			DayNumber: 1,
			Date:      startDate,
			Name:      "Day 1",
		},
	}

	exercises := []domain.PlannedExercise{
		{
			ID:            plannedExerciseID,
			DayID:         dayID,
			ExerciseID:    "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
			TargetSets:    3,
			TargetRepsMin: 5,
			TargetRepsMax: 8,
			TargetWeight:  100,
			TargetRPE:     8,
			Notes:         "",
			OrderIndex:    1,
		},
	}

	return plan, weeks, days, exercises
}

func TestReplaceActivePlan_Success(t *testing.T) {
	pool := setupTestDB(t)

	repo := postgres.NewPlannerRepo(
		pool,
		5*time.Second,
	)

	const userID = "11111111-1111-1111-1111-111111111111"

	ctx := context.Background()

	seedPlannerUser(
		t,
		repo,
		userID,
	)

	startDate := time.Date(
		2026,
		time.October,
		7,
		0,
		0,
		0,
		0,
		time.UTC,
	)

	oldPlan, oldWeeks, oldDays, oldExercises := makePlanGraph(
		userID,
		"Old plan",
		startDate,
		"21111111-1111-1111-1111-111111111111",
		"31111111-1111-1111-1111-111111111111",
		"41111111-1111-1111-1111-111111111111",
	)

	err := repo.CreatePlan(
		ctx,
		oldPlan,
		oldWeeks,
		oldDays,
		oldExercises,
	)
	require.NoError(t, err)

	newPlan, newWeeks, newDays, newExercises := makePlanGraph(
		userID,
		"New plan",
		startDate.AddDate(0, 1, 0),
		"51111111-1111-1111-1111-111111111111",
		"61111111-1111-1111-1111-111111111111",
		"71111111-1111-1111-1111-111111111111",
	)

	err = repo.ReplaceActivePlan(
		ctx,
		newPlan,
		newWeeks,
		newDays,
		newExercises,
	)
	require.NoError(t, err)

	var oldStatus string

	err = pool.QueryRow(
		ctx,
		`
			SELECT status
			FROM training_plans
			WHERE id = $1
		`,
		oldPlan.ID,
	).Scan(&oldStatus)

	require.NoError(t, err)
	require.Equal(t, "completed", oldStatus)

	var newStatus string

	err = pool.QueryRow(
		ctx,
		`
			SELECT status
			FROM training_plans
			WHERE id = $1
		`,
		newPlan.ID,
	).Scan(&newStatus)

	require.NoError(t, err)
	require.Equal(t, "active", newStatus)

	var activeCount int

	err = pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM training_plans
			WHERE user_id = $1
			  AND status = 'active'
		`,
		userID,
	).Scan(&activeCount)

	require.NoError(t, err)
	require.Equal(t, 1, activeCount)

	var weekCount int

	err = pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM plan_weeks
			WHERE plan_id = $1
		`,
		newPlan.ID,
	).Scan(&weekCount)

	require.NoError(t, err)
	require.Equal(t, 1, weekCount)

	var dayCount int

	err = pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM plan_days pd
			JOIN plan_weeks pw
				ON pd.week_id = pw.id
			WHERE pw.plan_id = $1
		`,
		newPlan.ID,
	).Scan(&dayCount)

	require.NoError(t, err)
	require.Equal(t, 1, dayCount)

	var exerciseCount int

	err = pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM planned_exercises pe
			JOIN plan_days pd
				ON pe.day_id = pd.id
			JOIN plan_weeks pw
				ON pd.week_id = pw.id
			WHERE pw.plan_id = $1
		`,
		newPlan.ID,
	).Scan(&exerciseCount)

	require.NoError(t, err)
	require.Equal(t, 1, exerciseCount)
}

func TestReplaceActivePlan_RollbackOnCreateFailure(
	t *testing.T,
) {
	pool := setupTestDB(t)

	repo := postgres.NewPlannerRepo(
		pool,
		5*time.Second,
	)

	const userID = "12222222-2222-2222-2222-222222222222"

	ctx := context.Background()

	seedPlannerUser(
		t,
		repo,
		userID,
	)

	startDate := time.Date(
		2026,
		time.October,
		7,
		0,
		0,
		0,
		0,
		time.UTC,
	)

	oldPlan, oldWeeks, oldDays, oldExercises := makePlanGraph(
		userID,
		"Old plan",
		startDate,
		"22222222-2222-2222-2222-222222222221",
		"32222222-2222-2222-2222-222222222222",
		"42222222-2222-2222-2222-222222222222",
	)

	err := repo.CreatePlan(
		ctx,
		oldPlan,
		oldWeeks,
		oldDays,
		oldExercises,
	)
	require.NoError(t, err)

	// Намеренно используем тот же week ID.
	//
	// Новый training_plan успеет INSERT-нуться,
	// старый active plan успеет стать completed,
	// но INSERT plan_weeks упадёт по PRIMARY KEY.
	//
	// После этого вся ReplaceActivePlan должна
	// откатиться целиком.
	newPlan, newWeeks, newDays, newExercises := makePlanGraph(
		userID,
		"Broken new plan",
		startDate.AddDate(0, 1, 0),
		oldWeeks[0].ID,
		"62222222-2222-2222-2222-222222222222",
		"72222222-2222-2222-2222-222222222222",
	)

	err = repo.ReplaceActivePlan(
		ctx,
		newPlan,
		newWeeks,
		newDays,
		newExercises,
	)
	require.Error(t, err)

	var oldStatus string

	err = pool.QueryRow(
		ctx,
		`
			SELECT status
			FROM training_plans
			WHERE id = $1
		`,
		oldPlan.ID,
	).Scan(&oldStatus)

	require.NoError(t, err)

	// Самая важная проверка:
	// UPDATE active -> completed тоже откатился.
	require.Equal(t, "active", oldStatus)

	var planCount int

	err = pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM training_plans
			WHERE user_id = $1
		`,
		userID,
	).Scan(&planCount)

	require.NoError(t, err)

	// Новый training_plan был INSERT-нут внутри tx,
	// но после ошибки week INSERT должен исчезнуть.
	require.Equal(t, 1, planCount)

	var activeCount int

	err = pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM training_plans
			WHERE user_id = $1
			  AND status = 'active'
		`,
		userID,
	).Scan(&activeCount)

	require.NoError(t, err)
	require.Equal(t, 1, activeCount)
}

func TestReplaceActivePlan_ConcurrentReplacements(
	t *testing.T,
) {
	pool := setupTestDB(t)

	repo := postgres.NewPlannerRepo(
		pool,
		5*time.Second,
	)

	const userID = "13333333-3333-3333-3333-333333333333"

	ctx := context.Background()

	seedPlannerUser(
		t,
		repo,
		userID,
	)

	startDate := time.Date(
		2026,
		time.October,
		7,
		0,
		0,
		0,
		0,
		time.UTC,
	)

	oldPlan, oldWeeks, oldDays, oldExercises := makePlanGraph(
		userID,
		"Initial plan",
		startDate,
		"23333333-3333-3333-3333-333333333331",
		"33333333-3333-3333-3333-333333333331",
		"43333333-3333-3333-3333-333333333331",
	)

	err := repo.CreatePlan(
		ctx,
		oldPlan,
		oldWeeks,
		oldDays,
		oldExercises,
	)
	require.NoError(t, err)

	planA, weeksA, daysA, exercisesA := makePlanGraph(
		userID,
		"Concurrent plan A",
		startDate.AddDate(0, 1, 0),
		"53333333-3333-3333-3333-333333333331",
		"63333333-3333-3333-3333-333333333331",
		"73333333-3333-3333-3333-333333333331",
	)

	planB, weeksB, daysB, exercisesB := makePlanGraph(
		userID,
		"Concurrent plan B",
		startDate.AddDate(0, 2, 0),
		"53333333-3333-3333-3333-333333333332",
		"63333333-3333-3333-3333-333333333332",
		"73333333-3333-3333-3333-333333333332",
	)

	start := make(chan struct{})

	errCh := make(chan error, 2)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()

		<-start

		errCh <- repo.ReplaceActivePlan(
			ctx,
			planA,
			weeksA,
			daysA,
			exercisesA,
		)
	}()

	go func() {
		defer wg.Done()

		<-start

		errCh <- repo.ReplaceActivePlan(
			ctx,
			planB,
			weeksB,
			daysB,
			exercisesB,
		)
	}()

	// Оба запроса начинают replacement почти одновременно.
	close(start)

	wg.Wait()
	close(errCh)

	for err := range errCh {
		require.NoError(t, err)
	}

	var activeCount int

	err = pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM training_plans
			WHERE user_id = $1
			  AND status = 'active'
		`,
		userID,
	).Scan(&activeCount)

	require.NoError(t, err)

	// Инвариант:
	// даже после двух concurrent replacement
	// active plan может быть только один.
	require.Equal(t, 1, activeCount)

	var totalPlans int

	err = pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM training_plans
			WHERE user_id = $1
		`,
		userID,
	).Scan(&totalPlans)

	require.NoError(t, err)

	// Initial + A + B.
	// Оба replacement успешно выполнились последовательно.
	require.Equal(t, 3, totalPlans)

	var completedCount int

	err = pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM training_plans
			WHERE user_id = $1
			  AND status = 'completed'
		`,
		userID,
	).Scan(&completedCount)

	require.NoError(t, err)

	require.Equal(t, 2, completedCount)

	var activeName string

	err = pool.QueryRow(
		ctx,
		`
			SELECT name
			FROM training_plans
			WHERE user_id = $1
			  AND status = 'active'
		`,
		userID,
	).Scan(&activeName)

	require.NoError(t, err)

	// Кто захватил lock последним — тот и останется active.
	require.Contains(
		t,
		[]string{
			"Concurrent plan A",
			"Concurrent plan B",
		},
		activeName,
	)
}
