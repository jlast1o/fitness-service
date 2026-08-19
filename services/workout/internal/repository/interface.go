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

	CreateOutboxEvent(ctx context.Context, event *domain.OutboxEvent) error
	ListPendingOutboxEvents(ctx context.Context, limit int) ([]domain.OutboxEvent, error)
	MarkOutboxEventPublished(ctx context.Context, eventID string) error
}
