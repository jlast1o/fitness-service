package service

import (
	"context"
	"errors"
	"time"

	"fitness-platform/pkg/logger"
	"fitness-platform/services/workout/internal/domain"
	"fitness-platform/services/workout/internal/repository"
)

// Специфические ошибки бизнес-логики.
var (
	ErrInvalidWorkoutData = errors.New("invalid workout data")
	ErrExerciseNotFound   = errors.New("exercise not found")
)

type WorkoutService struct {
	repo repository.WorkoutRepository
}

func NewWorkoutService(repo repository.WorkoutRepository) *WorkoutService {
	return &WorkoutService{repo: repo}
}

func (s *WorkoutService) CreateWorkout(ctx context.Context, userID string, name string, date time.Time, notes string, sets []domain.ExerciseSet) (*domain.Workout, error) {
	if userID == "" || name == "" || len(sets) == 0 {
		return nil, ErrInvalidWorkoutData
	}

	for _, set := range sets {
		ex, err := s.repo.GetExerciseByID(ctx, set.ExerciseID)
		if err != nil {
			logger.Log.Error().Err(err).Str("exercise_id", set.ExerciseID).Msg("failed to get exercise")
			return nil, err
		}
		if ex == nil {
			return nil, ErrExerciseNotFound
		}
	}

	workout := &domain.Workout{
		UserID:  userID,
		Name:    name,
		Date:    date,
		Notes:   notes,
		Metrics: map[string]any{},
	}

	for i := range sets {
		if sets[i].OrderIndex == 0 {
			sets[i].OrderIndex = i + 1
		}
		sets[i].Metrics = map[string]any{}
	}

	err := s.repo.CreateWorkout(ctx, workout, sets)
	if err != nil {
		logger.Log.Error().Err(err).Msg("failed to create workout")
		return nil, err
	}

	return workout, nil
}

func (s *WorkoutService) GetWorkout(ctx context.Context, workoutID string) (*domain.Workout, []domain.ExerciseSet, error) {
	if workoutID == "" {
		return nil, nil, ErrInvalidWorkoutData
	}
	workout, sets, err := s.repo.GetWorkoutByID(ctx, workoutID)
	if err != nil {
		logger.Log.Error().Err(err).Msg("failed to get workout")
		return nil, nil, err
	}
	if workout == nil {
		return nil, nil, nil // не найдено
	}
	return workout, sets, nil
}

func (s *WorkoutService) ListWorkouts(ctx context.Context, userID string, limit, offset int) ([]domain.Workout, error) {
	if userID == "" {
		return nil, ErrInvalidWorkoutData
	}
	if limit <= 0 || limit > 100 {
		limit = 20 // значение по умолчанию
	}
	if offset < 0 {
		offset = 0
	}
	workouts, err := s.repo.ListWorkoutsByUser(ctx, userID, limit, offset)
	if err != nil {
		logger.Log.Error().Err(err).Msg("failed to list workouts")
		return nil, err
	}
	return workouts, nil
}

func (s *WorkoutService) DeleteWorkout(ctx context.Context, workoutID string) error {
	if workoutID == "" {
		return ErrInvalidWorkoutData
	}
	return s.repo.DeleteWorkout(ctx, workoutID)
}

func (s *WorkoutService) UpdateWorkout(ctx context.Context, workout *domain.Workout) error {
	if workout == nil || workout.ID == "" {
		return ErrInvalidWorkoutData
	}
	return s.repo.UpdateWorkout(ctx, workout)
}

func (s *WorkoutService) ListExercises(ctx context.Context) ([]domain.Exercise, error) {
	return s.repo.ListExercises(ctx)
}

func (s *WorkoutService) CreateExercise(ctx context.Context, exercise *domain.Exercise) error {
	if exercise == nil || exercise.Name == "" || exercise.MuscleGroup == "" {
		return ErrInvalidWorkoutData
	}
	return s.repo.CreateExercise(ctx, exercise)
}
