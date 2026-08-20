package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fitness-platform/services/analytics/internal/domain"
	"fitness-platform/services/analytics/internal/repository"
)

// AnalyticsRepo реализует интерфейс repository.AnalyticsRepository.
type AnalyticsRepo struct {
	pool *pgxpool.Pool
}

// NewAnalyticsRepo создаёт новый экземпляр AnalyticsRepo.
func NewAnalyticsRepo(pool *pgxpool.Pool) repository.AnalyticsRepository {
	return &AnalyticsRepo{pool: pool}
}

// UpsertUserStats вставляет или обновляет агрегированную статистику пользователя.
func (r *AnalyticsRepo) UpsertUserStats(ctx context.Context, stats *domain.UserStats) error {
	query := `
		INSERT INTO user_stats (user_id, total_workouts, total_volume, avg_intensity, updated_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (user_id)
		DO UPDATE SET
			total_workouts = EXCLUDED.total_workouts,
			total_volume = EXCLUDED.total_volume,
			avg_intensity = EXCLUDED.avg_intensity,
			updated_at = NOW()
	`
	_, err := r.pool.Exec(ctx, query,
		stats.UserID,
		stats.TotalWorkouts,
		stats.TotalVolume,
		stats.AvgIntensity,
	)
	if err != nil {
		return fmt.Errorf("upsert user stats: %w", err)
	}
	return nil
}

// GetUserStats возвращает статистику пользователя по ID.
func (r *AnalyticsRepo) GetUserStats(ctx context.Context, userID string) (*domain.UserStats, error) {
	query := `
		SELECT user_id, total_workouts, total_volume, avg_intensity, updated_at
		FROM user_stats
		WHERE user_id = $1
	`
	stats := &domain.UserStats{}
	err := r.pool.QueryRow(ctx, query, userID).Scan(
		&stats.UserID,
		&stats.TotalWorkouts,
		&stats.TotalVolume,
		&stats.AvgIntensity,
		&stats.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get user stats: %w", err)
	}
	return stats, nil
}

// UpsertExerciseProgress вставляет или обновляет прогресс по упражнению.
func (r *AnalyticsRepo) UpsertExerciseProgress(ctx context.Context, progress *domain.ExerciseProgress) error {
	query := `
		INSERT INTO exercise_progress (
			user_id, exercise_id, best_weight, total_reps, last_workout_at, estimated_1rm, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (user_id, exercise_id)
		DO UPDATE SET
			best_weight = EXCLUDED.best_weight,
			total_reps = EXCLUDED.total_reps,
			last_workout_at = EXCLUDED.last_workout_at,
			estimated_1rm = EXCLUDED.estimated_1rm,
			updated_at = NOW()
	`
	_, err := r.pool.Exec(ctx, query,
		progress.UserID,
		progress.ExerciseID,
		progress.BestWeight,
		progress.TotalReps,
		progress.LastWorkoutAt,
		progress.Estimated1RM,
	)
	if err != nil {
		return fmt.Errorf("upsert exercise progress: %w", err)
	}
	return nil
}

// GetExerciseProgress возвращает прогресс по конкретному упражнению.
func (r *AnalyticsRepo) GetExerciseProgress(ctx context.Context, userID, exerciseID string) (*domain.ExerciseProgress, error) {
	query := `
		SELECT user_id, exercise_id, best_weight, total_reps, last_workout_at, estimated_1rm, updated_at
		FROM exercise_progress
		WHERE user_id = $1 AND exercise_id = $2
	`
	progress := &domain.ExerciseProgress{}
	err := r.pool.QueryRow(ctx, query, userID, exerciseID).Scan(
		&progress.UserID,
		&progress.ExerciseID,
		&progress.BestWeight,
		&progress.TotalReps,
		&progress.LastWorkoutAt,
		&progress.Estimated1RM,
		&progress.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get exercise progress: %w", err)
	}
	return progress, nil
}

// ListExerciseProgress возвращает прогресс по всем упражнениям пользователя.
func (r *AnalyticsRepo) ListExerciseProgress(ctx context.Context, userID string) ([]domain.ExerciseProgress, error) {
	query := `
		SELECT user_id, exercise_id, best_weight, total_reps, last_workout_at, estimated_1rm, updated_at
		FROM exercise_progress
		WHERE user_id = $1
		ORDER BY best_weight DESC
	`
	rows, err := r.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("list exercise progress: %w", err)
	}
	defer rows.Close()

	var progressList []domain.ExerciseProgress
	for rows.Next() {
		var p domain.ExerciseProgress
		if err := rows.Scan(
			&p.UserID,
			&p.ExerciseID,
			&p.BestWeight,
			&p.TotalReps,
			&p.LastWorkoutAt,
			&p.Estimated1RM,
			&p.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan exercise progress: %w", err)
		}
		progressList = append(progressList, p)
	}
	return progressList, rows.Err()
}

// InsertWorkoutSummary вставляет сводку тренировки, игнорируя дубликаты.
func (r *AnalyticsRepo) InsertWorkoutSummary(ctx context.Context, summary *domain.WorkoutSummary) error {
	query := `
		INSERT INTO workout_summary (workout_id, user_id, name, date, total_volume, set_count, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (workout_id) DO NOTHING
	`
	_, err := r.pool.Exec(ctx, query,
		summary.WorkoutID,
		summary.UserID,
		summary.Name,
		summary.Date,
		summary.TotalVolume,
		summary.SetCount,
	)
	if err != nil {
		return fmt.Errorf("insert workout summary: %w", err)
	}
	return nil
}

// ListWorkoutSummaries возвращает список сводок тренировок пользователя.
func (r *AnalyticsRepo) ListWorkoutSummaries(ctx context.Context, userID string, limit, offset int) ([]domain.WorkoutSummary, error) {
	query := `
		SELECT workout_id, user_id, name, date, total_volume, set_count, created_at
		FROM workout_summary
		WHERE user_id = $1
		ORDER BY date DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := r.pool.Query(ctx, query, userID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list workout summaries: %w", err)
	}
	defer rows.Close()

	var summaries []domain.WorkoutSummary
	for rows.Next() {
		var s domain.WorkoutSummary
		if err := rows.Scan(
			&s.WorkoutID,
			&s.UserID,
			&s.Name,
			&s.Date,
			&s.TotalVolume,
			&s.SetCount,
			&s.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan workout summary: %w", err)
		}
		summaries = append(summaries, s)
	}
	return summaries, rows.Err()
}
