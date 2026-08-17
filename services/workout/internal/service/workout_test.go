package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"fitness-platform/services/workout/internal/domain"
	"fitness-platform/services/workout/internal/service"
)

// MockWorkoutRepo — мок-реализация интерфейса repository.WorkoutRepository.
type MockWorkoutRepo struct {
	mock.Mock
}

func (m *MockWorkoutRepo) CreateWorkout(ctx context.Context, workout *domain.Workout, sets []domain.ExerciseSet) error {
	args := m.Called(ctx, workout, sets)
	return args.Error(0)
}

func (m *MockWorkoutRepo) GetWorkoutByID(ctx context.Context, workoutID string) (*domain.Workout, []domain.ExerciseSet, error) {
	args := m.Called(ctx, workoutID)
	// Разбираем возвращаемые значения
	var w *domain.Workout
	if args.Get(0) != nil {
		w = args.Get(0).(*domain.Workout)
	}
	var s []domain.ExerciseSet
	if args.Get(1) != nil {
		s = args.Get(1).([]domain.ExerciseSet)
	}
	return w, s, args.Error(2)
}

func (m *MockWorkoutRepo) ListWorkoutsByUser(ctx context.Context, userID string, limit, offset int) ([]domain.Workout, error) {
	args := m.Called(ctx, userID, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.Workout), args.Error(1)
}

func (m *MockWorkoutRepo) DeleteWorkout(ctx context.Context, workoutID string) error {
	args := m.Called(ctx, workoutID)
	return args.Error(0)
}

func (m *MockWorkoutRepo) UpdateWorkout(ctx context.Context, workout *domain.Workout) error {
	args := m.Called(ctx, workout)
	return args.Error(0)
}

func (m *MockWorkoutRepo) ListExercises(ctx context.Context) ([]domain.Exercise, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.Exercise), args.Error(1)
}

func (m *MockWorkoutRepo) CreateExercise(ctx context.Context, exercise *domain.Exercise) error {
	args := m.Called(ctx, exercise)
	return args.Error(0)
}

func (m *MockWorkoutRepo) GetExerciseByID(ctx context.Context, exerciseID string) (*domain.Exercise, error) {
	args := m.Called(ctx, exerciseID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Exercise), args.Error(1)
}

// Тесты

func TestCreateWorkout_Success(t *testing.T) {
	mockRepo := new(MockWorkoutRepo)
	svc := service.NewWorkoutService(mockRepo)

	exercise := &domain.Exercise{ID: "ex1", Name: "Bench Press"}
	sets := []domain.ExerciseSet{
		{ExerciseID: "ex1", Weight: 100, Reps: 5, RPE: 8},
	}

	// Ожидаем проверку существования упражнения
	mockRepo.On("GetExerciseByID", mock.Anything, "ex1").Return(exercise, nil)
	// Ожидаем создание тренировки с заполненным workout
	mockRepo.On("CreateWorkout", mock.Anything, mock.MatchedBy(func(w *domain.Workout) bool {
		return w.UserID == "user1" && w.Name == "Workout A"
	}), mock.Anything).Return(nil).Run(func(args mock.Arguments) {
		w := args.Get(1).(*domain.Workout)
		w.ID = "workout1"
		w.CreatedAt = time.Now()
		w.UpdatedAt = time.Now()
	})

	workout, err := svc.CreateWorkout(context.Background(), "user1", "Workout A", time.Now(), "", sets)

	require.NoError(t, err)
	assert.Equal(t, "workout1", workout.ID)
	mockRepo.AssertExpectations(t)
}

func TestCreateWorkout_InvalidData(t *testing.T) {
	mockRepo := new(MockWorkoutRepo)
	svc := service.NewWorkoutService(mockRepo)

	_, err := svc.CreateWorkout(context.Background(), "", "Workout A", time.Now(), "", []domain.ExerciseSet{})
	require.Error(t, err)
	assert.ErrorIs(t, err, service.ErrInvalidWorkoutData)
	mockRepo.AssertNotCalled(t, "CreateWorkout")
}

func TestCreateWorkout_ExerciseNotFound(t *testing.T) {
	mockRepo := new(MockWorkoutRepo)
	svc := service.NewWorkoutService(mockRepo)

	sets := []domain.ExerciseSet{
		{ExerciseID: "nonexistent", Weight: 100, Reps: 5},
	}

	mockRepo.On("GetExerciseByID", mock.Anything, "nonexistent").Return(nil, nil)

	_, err := svc.CreateWorkout(context.Background(), "user1", "Workout A", time.Now(), "", sets)
	require.Error(t, err)
	assert.ErrorIs(t, err, service.ErrExerciseNotFound)
	mockRepo.AssertNotCalled(t, "CreateWorkout")
}

func TestGetWorkout_Success(t *testing.T) {
	mockRepo := new(MockWorkoutRepo)
	svc := service.NewWorkoutService(mockRepo)

	expectedWorkout := &domain.Workout{ID: "w1", UserID: "user1", Name: "Workout A"}
	expectedSets := []domain.ExerciseSet{{ID: "s1"}}

	mockRepo.On("GetWorkoutByID", mock.Anything, "w1").Return(expectedWorkout, expectedSets, nil)

	workout, sets, err := svc.GetWorkout(context.Background(), "user1", "w1")

	require.NoError(t, err)
	assert.Equal(t, expectedWorkout, workout)
	assert.Equal(t, expectedSets, sets)
	mockRepo.AssertExpectations(t)
}

func TestGetWorkout_Forbidden(t *testing.T) {
	mockRepo := new(MockWorkoutRepo)
	svc := service.NewWorkoutService(mockRepo)

	workout := &domain.Workout{ID: "w1", UserID: "user2"} // принадлежит другому
	mockRepo.On("GetWorkoutByID", mock.Anything, "w1").Return(workout, nil, nil)

	_, _, err := svc.GetWorkout(context.Background(), "user1", "w1")

	require.Error(t, err)
	assert.ErrorIs(t, err, service.ErrForbbiden)
}

func TestDeleteWorkout_Forbidden(t *testing.T) {
	mockRepo := new(MockWorkoutRepo)
	svc := service.NewWorkoutService(mockRepo)

	workout := &domain.Workout{ID: "w1", UserID: "user2"}
	mockRepo.On("GetWorkoutByID", mock.Anything, "w1").Return(workout, nil, nil)

	err := svc.DeleteWorkout(context.Background(), "user1", "w1")

	require.Error(t, err)
	assert.ErrorIs(t, err, service.ErrForbbiden)
	mockRepo.AssertNotCalled(t, "DeleteWorkout")
}

func TestDeleteWorkout_Success(t *testing.T) {
	mockRepo := new(MockWorkoutRepo)
	svc := service.NewWorkoutService(mockRepo)

	workout := &domain.Workout{ID: "w1", UserID: "user1"}
	mockRepo.On("GetWorkoutByID", mock.Anything, "w1").Return(workout, nil, nil)
	mockRepo.On("DeleteWorkout", mock.Anything, "w1").Return(nil)

	err := svc.DeleteWorkout(context.Background(), "user1", "w1")

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

func TestUpdateWorkout_Forbidden(t *testing.T) {
	mockRepo := new(MockWorkoutRepo)
	svc := service.NewWorkoutService(mockRepo)

	existing := &domain.Workout{ID: "w1", UserID: "user2"}
	mockRepo.On("GetWorkoutByID", mock.Anything, "w1").Return(existing, nil, nil)

	updated := &domain.Workout{ID: "w1", UserID: "user1", Name: "Updated"}
	err := svc.UpdateWorkout(context.Background(), "user1", updated)

	require.Error(t, err)
	assert.ErrorIs(t, err, service.ErrForbbiden)
	mockRepo.AssertNotCalled(t, "UpdateWorkout")
}

func TestUpdateWorkout_Success(t *testing.T) {
	mockRepo := new(MockWorkoutRepo)
	svc := service.NewWorkoutService(mockRepo)

	existing := &domain.Workout{ID: "w1", UserID: "user1", Name: "Old"}
	mockRepo.On("GetWorkoutByID", mock.Anything, "w1").Return(existing, nil, nil)
	mockRepo.On("UpdateWorkout", mock.Anything, mock.MatchedBy(func(w *domain.Workout) bool {
		return w.ID == "w1" && w.UserID == "user1" && w.Name == "Updated"
	})).Return(nil)

	updated := &domain.Workout{ID: "w1", UserID: "user1", Name: "Updated"}
	err := svc.UpdateWorkout(context.Background(), "user1", updated)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

func TestListWorkouts(t *testing.T) {
	mockRepo := new(MockWorkoutRepo)
	svc := service.NewWorkoutService(mockRepo)

	workouts := []domain.Workout{{ID: "w1"}, {ID: "w2"}}
	mockRepo.On("ListWorkoutsByUser", mock.Anything, "user1", 20, 0).Return(workouts, nil)

	result, err := svc.ListWorkouts(context.Background(), "user1", 0, 0) // limit=0 => default 20, offset=0

	require.NoError(t, err)
	assert.Len(t, result, 2)
	mockRepo.AssertExpectations(t)
}

func TestListExercises(t *testing.T) {
	mockRepo := new(MockWorkoutRepo)
	svc := service.NewWorkoutService(mockRepo)

	exercises := []domain.Exercise{{ID: "ex1", Name: "Bench Press"}}
	mockRepo.On("ListExercises", mock.Anything).Return(exercises, nil)

	result, err := svc.ListExercises(context.Background())

	require.NoError(t, err)
	assert.Len(t, result, 1)
}

func TestCreateExercise_InvalidData(t *testing.T) {
	mockRepo := new(MockWorkoutRepo)
	svc := service.NewWorkoutService(mockRepo)

	err := svc.CreateExercise(context.Background(), &domain.Exercise{Name: "", MuscleGroup: ""})
	require.Error(t, err)
	assert.ErrorIs(t, err, service.ErrInvalidWorkoutData)
	mockRepo.AssertNotCalled(t, "CreateExercise")
}

func TestCreateExercise_Success(t *testing.T) {
	mockRepo := new(MockWorkoutRepo)
	svc := service.NewWorkoutService(mockRepo)

	exercise := &domain.Exercise{Name: "Bench Press", MuscleGroup: "Chest"}
	mockRepo.On("CreateExercise", mock.Anything, exercise).Return(nil).Run(func(args mock.Arguments) {
		e := args.Get(1).(*domain.Exercise)
		e.ID = "new-ex"
	})

	err := svc.CreateExercise(context.Background(), exercise)

	require.NoError(t, err)
	assert.NotEmpty(t, exercise.ID)
	mockRepo.AssertExpectations(t)
}
