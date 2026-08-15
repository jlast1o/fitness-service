package repository

import (
	"context"
	"fitness-platform/services/workout/internal/domain"
)

type WorkoutRepository interface {
	CreateWorkout(ctx context.Context, workout *domain.Workout, sets []domain.ExerciseSet) error
	GetWorkoutByID(ctx context.Context, workoutID string) (*domain.Workout, []domain.ExerciseSet, error)
	ListWorkoutsByUser(ctx context.Context, userID string, limit, offset int) ([]domain.Workout, error)
	DeleteWorkout(ctx context.Context, workoutID string) error
	UpdateWorkout(ctx context.Context, workout *domain.Workout) error

	ListExercises(ctx context.Context) ([]domain.Exercise, error)
	CreateExercise(ctx context.Context, exercise *domain.Exercise) error
	GetExerciseByID(ctx context.Context, exerciseID string) (*domain.Exercise, error)
}
