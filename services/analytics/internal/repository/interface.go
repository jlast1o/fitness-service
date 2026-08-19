package repository

import (
	"context"

	"fitness-platform/services/analytics/internal/domain"
)

// AnalyticsRepository определяет контракт для работы с аналитикой.
type AnalyticsRepository interface {
	UpsertUserStats(ctx context.Context, stats *domain.UserStats) error
	GetUserStats(ctx context.Context, userID string) (*domain.UserStats, error)

	UpsertExerciseProgress(ctx context.Context, progress *domain.ExerciseProgress) error
	GetExerciseProgress(ctx context.Context, userID, exerciseID string) (*domain.ExerciseProgress, error)
	ListExerciseProgress(ctx context.Context, userID string) ([]domain.ExerciseProgress, error)

	InsertWorkoutSummary(ctx context.Context, summary *domain.WorkoutSummary) error
	ListWorkoutSummaries(ctx context.Context, userID string, limit, offset int) ([]domain.WorkoutSummary, error)
}
