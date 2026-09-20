package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"

	"fitness-platform/pkg/logger"
	"fitness-platform/services/workout/internal/domain"
	"fitness-platform/services/workout/internal/repository"
)

// Специфические ошибки бизнес-логики.
var (
	ErrInvalidWorkoutData = errors.New("invalid workout data")
	ErrExerciseNotFound   = errors.New("exercise not found")
	ErrForbbiden          = errors.New("user are not allowed to perform this action")
)

const exercisesCacheKey = "workout:exercises:all"

type WorkoutService struct {
	repo             repository.WorkoutRepository
	redisClient      redis.Cmdable
	exerciseCacheTTL time.Duration
}

type Option func(*WorkoutService)

func WithRedis(client redis.Cmdable, cacheTTL time.Duration) Option {
	return func(s *WorkoutService) {
		s.redisClient = client
		s.exerciseCacheTTL = cacheTTL
	}
}

func NewWorkoutService(repo repository.WorkoutRepository, opts ...Option) *WorkoutService {
	s := &WorkoutService{
		repo: repo,
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
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

func (s *WorkoutService) GetWorkout(ctx context.Context, userID, workoutID string) (*domain.Workout, []domain.ExerciseSet, error) {
	if workoutID == "" || userID == "" {
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
	if userID != workout.UserID {
		return nil, nil, ErrForbbiden
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

func (s *WorkoutService) DeleteWorkout(ctx context.Context, userID, workoutID string) error {
	if workoutID == "" || userID == "" {
		return ErrInvalidWorkoutData
	}
	workout, _, err := s.repo.GetWorkoutByID(ctx, workoutID)
	if err != nil {
		logger.Log.Error().Err(err).Msg("failed to get workout for deletion")
		return err
	}

	if workout == nil {
		return nil
	}

	if workout.UserID != userID {
		return ErrForbbiden
	}
	return s.repo.DeleteWorkout(ctx, workoutID)
}

func (s *WorkoutService) UpdateWorkout(ctx context.Context, userID string, workout *domain.Workout) error {
	if workout == nil || workout.ID == "" || userID == "" {
		return ErrInvalidWorkoutData
	}

	existing, _, err := s.repo.GetWorkoutByID(ctx, workout.ID)
	if err != nil {
		logger.Log.Error().Err(err).Msg("failed to get workout to update")
		return err
	}

	if existing == nil {
		return ErrInvalidWorkoutData
	}

	if existing.UserID != userID {
		return ErrForbbiden
	}

	workout.UserID = existing.UserID

	return s.repo.UpdateWorkout(ctx, workout)
}

func (s *WorkoutService) ListExercises(ctx context.Context) ([]domain.Exercise, error) {
	if s.redisClient != nil {
		cached, err := s.redisClient.Get(ctx, exercisesCacheKey).Bytes()

		if err == nil {
			var exercises []domain.Exercise

			if err := json.Unmarshal(cached, &exercises); err == nil {
				return exercises, nil
			} else {
				logger.Log.Warn().
					Err(err).
					Msg("failed to unmarshal exercises cache")
			}
		} else if !errors.Is(err, redis.Nil) {
			logger.Log.Warn().
				Err(err).
				Msg("failed to read exercises cache")
		}
	}

	exercises, err := s.repo.ListExercises(ctx)
	if err != nil {
		return nil, err
	}

	if s.redisClient != nil {
		data, err := json.Marshal(exercises)
		if err != nil {
			logger.Log.Warn().
				Err(err).
				Msg("failed to marshal exercise cache")
		} else if err := s.redisClient.Set(
			ctx,
			exercisesCacheKey,
			data,
			s.exerciseCacheTTL,
		).Err(); err != nil {
			logger.Log.Warn().
				Err(err).
				Msg("failed to write exercises cache")
		}
	}

	return exercises, nil
}

func (s *WorkoutService) CreateExercise(ctx context.Context, exercise *domain.Exercise) error {
	if exercise == nil || exercise.Name == "" || exercise.MuscleGroup == "" {
		return ErrInvalidWorkoutData
	}

	if err := s.repo.CreateExercise(ctx, exercise); err != nil {
		return err
	}

	if s.redisClient != nil {
		if err := s.redisClient.Del(ctx, exercisesCacheKey).Err(); err != nil {
			logger.Log.Warn().
				Err(err).
				Msg("failed to invalidate exercises cache")
		}
	}

	return nil
}
