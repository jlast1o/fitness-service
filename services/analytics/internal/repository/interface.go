package repository

import (
	"context"

	"fitness-platform/services/analytics/internal/domain"
)

// AnalyticsEventTx описывает операции,
// которые могут выполняться внутри транзакции обработки события.
type AnalyticsEventTx interface {
	AccumulateUserStats(
		ctx context.Context,
		userID string,
		totalVolume float64,
		totalReps int,
	) error

	MergeExerciseProgress(
		ctx context.Context,
		progress *domain.ExerciseProgress,
	) error

	InsertWorkoutSummary(
		ctx context.Context,
		summary *domain.WorkoutSummary,
	) error
}

// AnalyticsRepository определяет контракт для работы с аналитикой.
type AnalyticsRepository interface {
	UpsertUserStats(ctx context.Context, stats *domain.UserStats) error
	GetUserStats(ctx context.Context, userID string) (*domain.UserStats, error)
	UpsertExerciseProgress(ctx context.Context, progress *domain.ExerciseProgress) error
	GetExerciseProgress(ctx context.Context, userID, exerciseID string) (*domain.ExerciseProgress, error)
	ListExerciseProgress(ctx context.Context, userID string) ([]domain.ExerciseProgress, error)
	InsertWorkoutSummary(ctx context.Context, summary *domain.WorkoutSummary) error
	ListWorkoutSummaries(ctx context.Context, userID string, limit, offset int) ([]domain.WorkoutSummary, error)

	// WithEventTransaction атомарно:
	// 1. пытается зарегистрировать eventID в processed_events;
	// 2. если событие новое — выполняет fn;
	// 3. коммитит processed_events и бизнес-изменения вместе.
	//
	// applied == false означает, что eventID уже был обработан.
	WithEventTransaction(
		ctx context.Context,
		eventID string,
		fn func(context.Context, AnalyticsEventTx) error,
	) (applied bool, err error)
}
