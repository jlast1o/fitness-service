package postgres_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"fitness-platform/services/workout/internal/domain"
	"fitness-platform/services/workout/internal/repository/postgres"
)

const (
	userID1 = "11111111-1111-1111-1111-111111111111"
	userID2 = "22222222-2222-2222-2222-222222222222"
)

// setupTestDB поднимает контейнер PostgreSQL, применяет миграции и возвращает пул + cleanup.
func setupTestDB(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()
	// Исправление для Windows: заставляем testcontainers использовать IPv4
	t.Setenv("TESTCONTAINERS_HOST_OVERRIDE", "127.0.0.1")
	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        "postgres:15-alpine",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "test",
			"POSTGRES_PASSWORD": "test",
			"POSTGRES_DB":       "testdb",
		},
		WaitingFor: wait.ForListeningPort("5432/tcp"),
	}

	postgresContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)

	host, err := postgresContainer.Host(ctx)
	require.NoError(t, err)
	port, err := postgresContainer.MappedPort(ctx, "5432")
	require.NoError(t, err)

	dbURL := fmt.Sprintf("postgres://test:test@%s:%s/testdb?sslmode=disable", host, port.Port())

	// Небольшая задержка для полной готовности PostgreSQL
	time.Sleep(2 * time.Second)

	// Применяем миграции Workout (путь: от текущего пакета до services/workout/migrations)
	m, err := migrate.New("file://../../../migrations", dbURL)
	require.NoError(t, err)
	err = m.Up()
	require.NoError(t, err)

	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)

	cleanup := func() {
		pool.Close()
		if err := postgresContainer.Terminate(ctx); err != nil {
			t.Logf("failed to terminate container: %v", err)
		}
	}

	return pool, cleanup
}

// Вспомогательная функция для создания упражнения и возврата его ID.
func createTestExercise(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	ctx := context.Background()
	exercise := &domain.Exercise{
		Name:        "Bench Press",
		MuscleGroup: "Chest",
		Category:    "Compound",
	}
	// Используем репозиторий напрямую
	repo := postgres.NewWorkoutRepo(pool)
	err := repo.CreateExercise(ctx, exercise)
	require.NoError(t, err)
	require.NotEmpty(t, exercise.ID)
	return exercise.ID
}

func TestCreateWorkout_Success(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	repo := postgres.NewWorkoutRepo(pool)

	exerciseID := createTestExercise(t, pool)

	workout := &domain.Workout{
		UserID:  userID1,
		Name:    "Workout A",
		Date:    time.Now(),
		Notes:   "Good",
		Metrics: map[string]any{"mood": "high"},
	}

	sets := []domain.ExerciseSet{
		{
			ExerciseID: exerciseID,
			OrderIndex: 1,
			Weight:     100,
			Reps:       5,
			RPE:        8,
			Metrics:    map[string]any{"speed": "fast"},
		},
	}

	err := repo.CreateWorkout(ctx, workout, sets)
	require.NoError(t, err)
	require.NotEmpty(t, workout.ID)
	require.False(t, workout.CreatedAt.IsZero())

	// Проверяем, что тренировка действительно сохранена и подходы на месте
	gotWorkout, gotSets, err := repo.GetWorkoutByID(ctx, workout.ID)
	require.NoError(t, err)
	require.NotNil(t, gotWorkout)
	require.Equal(t, workout.ID, gotWorkout.ID)
	require.Len(t, gotSets, 1)
	require.Equal(t, exerciseID, gotSets[0].ExerciseID)
	require.Equal(t, 100.0, gotSets[0].Weight)
}

func TestGetWorkoutByID_NotFound(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	repo := postgres.NewWorkoutRepo(pool)
	workout, sets, err := repo.GetWorkoutByID(context.Background(), "00000000-0000-0000-0000-000000000000")
	require.NoError(t, err)
	require.Nil(t, workout)
	require.Nil(t, sets)
}

func TestDeleteWorkout_Cascade(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	repo := postgres.NewWorkoutRepo(pool)

	exerciseID := createTestExercise(t, pool)

	workout := &domain.Workout{
		UserID: userID1,
		Name:   "Workout to delete",
		Date:   time.Now(),
	}
	sets := []domain.ExerciseSet{
		{ExerciseID: exerciseID, OrderIndex: 1, Weight: 80, Reps: 10},
	}
	err := repo.CreateWorkout(ctx, workout, sets)
	require.NoError(t, err)

	// Удаляем тренировку
	err = repo.DeleteWorkout(ctx, workout.ID)
	require.NoError(t, err)

	// Проверяем, что тренировка исчезла
	gotWorkout, _, err := repo.GetWorkoutByID(ctx, workout.ID)
	require.NoError(t, err)
	require.Nil(t, gotWorkout)

	// Проверяем, что подходы каскадно удалены
	// Для этого нужно отдельно проверить, что в таблице exercise_sets нет записей с workout_id
	// Так как метода в репозитории нет, выполним прямой SQL
	var count int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM exercise_sets WHERE workout_id = $1`, workout.ID).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 0, count)
}

func TestListWorkoutsByUser(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	repo := postgres.NewWorkoutRepo(pool)

	exerciseID := createTestExercise(t, pool)

	// Создаём две тренировки для одного пользователя
	w1 := &domain.Workout{UserID: userID1, Name: "Workout 1", Date: time.Now().Add(-48 * time.Hour)}
	w2 := &domain.Workout{UserID: userID1, Name: "Workout 2", Date: time.Now()}
	sets1 := []domain.ExerciseSet{{ExerciseID: exerciseID, Weight: 60, Reps: 12}}
	sets2 := []domain.ExerciseSet{{ExerciseID: exerciseID, Weight: 70, Reps: 10}}

	err := repo.CreateWorkout(ctx, w1, sets1)
	require.NoError(t, err)
	err = repo.CreateWorkout(ctx, w2, sets2)
	require.NoError(t, err)

	workouts, err := repo.ListWorkoutsByUser(ctx, userID1, 10, 0)
	require.NoError(t, err)
	require.Len(t, workouts, 2)
	// Проверяем сортировку по дате DESC (w2 раньше)
	require.Equal(t, w2.ID, workouts[0].ID)
}

func TestUpdateWorkout(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	repo := postgres.NewWorkoutRepo(pool)

	exerciseID := createTestExercise(t, pool)

	workout := &domain.Workout{UserID: userID1, Name: "Old Name", Date: time.Now()}
	sets := []domain.ExerciseSet{{ExerciseID: exerciseID, Weight: 50, Reps: 8}}
	err := repo.CreateWorkout(ctx, workout, sets)
	require.NoError(t, err)

	// Обновляем имя и заметки
	workout.Name = "New Name"
	workout.Notes = "Updated"
	workout.Metrics = map[string]any{"updated": true}
	err = repo.UpdateWorkout(ctx, workout)
	require.NoError(t, err)

	// Проверяем, что обновилось
	gotWorkout, _, err := repo.GetWorkoutByID(ctx, workout.ID)
	require.NoError(t, err)
	require.Equal(t, "New Name", gotWorkout.Name)
	require.Equal(t, "Updated", gotWorkout.Notes)
	require.Equal(t, map[string]any{"updated": true}, gotWorkout.Metrics)
}

func TestListExercises(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	repo := postgres.NewWorkoutRepo(pool)

	// Создаём несколько упражнений
	ex1 := &domain.Exercise{Name: "Squat", MuscleGroup: "Legs", Category: "Compound"}
	ex2 := &domain.Exercise{Name: "Curl", MuscleGroup: "Arms", Category: "Isolation"}
	require.NoError(t, repo.CreateExercise(context.Background(), ex1))
	require.NoError(t, repo.CreateExercise(context.Background(), ex2))

	exercises, err := repo.ListExercises(context.Background())
	require.NoError(t, err)
	require.Len(t, exercises, 2)
}
