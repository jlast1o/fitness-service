package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fitness-platform/services/analytics/internal/domain"
	"fitness-platform/services/analytics/internal/repository"
)

// AnalyticsRepo реализует интерфейс repository.AnalyticsRepository.
type AnalyticsRepo struct {
	pool             *pgxpool.Pool
	operationTimeout time.Duration
}

// NewAnalyticsRepo создаёт новый экземпляр AnalyticsRepo.
func NewAnalyticsRepo(pool *pgxpool.Pool, operationTimeout time.Duration) repository.AnalyticsRepository {
	return &AnalyticsRepo{
		pool:             pool,
		operationTimeout: operationTimeout,
	}
}

// UpsertUserStats вставляет или обновляет агрегированную статистику пользователя.
func (r *AnalyticsRepo) UpsertUserStats(
	ctx context.Context,
	stats *domain.UserStats,
) error {
	ctx, cancel := r.operationCtx(ctx)
	defer cancel()

	query := `
		INSERT INTO user_stats (
			user_id,
			total_workouts,
			total_volume,
			total_reps,
			avg_intensity,
			updated_at
		)
		VALUES ($1, $2, $3, $4, $5, NOW())
		ON CONFLICT (user_id)
		DO UPDATE SET
			total_workouts = EXCLUDED.total_workouts,
			total_volume = EXCLUDED.total_volume,
			total_reps = EXCLUDED.total_reps,
			avg_intensity = EXCLUDED.avg_intensity,
			updated_at = NOW()
	`

	_, err := r.pool.Exec(
		ctx,
		query,
		stats.UserID,
		stats.TotalWorkouts,
		stats.TotalVolume,
		stats.TotalReps,
		stats.AvgIntensity,
	)
	if err != nil {
		return fmt.Errorf(
			"upsert user stats: %w",
			err,
		)
	}

	return nil
}

// GetUserStats возвращает статистику пользователя по ID.
func (r *AnalyticsRepo) GetUserStats(
	ctx context.Context,
	userID string,
) (*domain.UserStats, error) {
	ctx, cancel := r.operationCtx(ctx)
	defer cancel()

	query := `
		SELECT
			user_id,
			total_workouts,
			total_volume,
			total_reps,
			avg_intensity,
			updated_at
		FROM user_stats
		WHERE user_id = $1
	`

	stats := &domain.UserStats{}

	err := r.pool.QueryRow(
		ctx,
		query,
		userID,
	).Scan(
		&stats.UserID,
		&stats.TotalWorkouts,
		&stats.TotalVolume,
		&stats.TotalReps,
		&stats.AvgIntensity,
		&stats.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}

		return nil, fmt.Errorf(
			"get user stats: %w",
			err,
		)
	}

	return stats, nil
}

// UpsertExerciseProgress вставляет или обновляет прогресс по упражнению.
func (r *AnalyticsRepo) UpsertExerciseProgress(ctx context.Context, progress *domain.ExerciseProgress) error {
	ctx, cancel := r.operationCtx(ctx)
	defer cancel()

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
	ctx, cancel := r.operationCtx(ctx)
	defer cancel()

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
	ctx, cancel := r.operationCtx(ctx)
	defer cancel()

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
	ctx, cancel := r.operationCtx(ctx)
	defer cancel()

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
	ctx, cancel := r.operationCtx(ctx)
	defer cancel()

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

type analyticsEventTx struct {
	tx pgx.Tx
}

// WithEventTransaction выполняет обработку события
// и запись event_id в одной PostgreSQL-транзакции.
func (r *AnalyticsRepo) WithEventTransaction(ctx context.Context, eventID string,
	fn func(context.Context, repository.AnalyticsEventTx) error) (bool, error) {
	ctx, cancel := r.operationCtx(ctx)
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin event transaction: %w", err)
	}

	defer func() {
		_ = tx.Rollback(context.Background())
	}()

	query := `
		INSERT INTO processed_events (event_id)
		VALUES ($1)
		ON CONFLICT (event_id) DO NOTHING
	`

	commandTag, err := tx.Exec(
		ctx,
		query,
		eventID,
	)
	if err != nil {
		return false, fmt.Errorf("claim processed event: %w", err)
	}

	if commandTag.RowsAffected() == 0 {
		return false, nil
	}

	eventTx := &analyticsEventTx{
		tx: tx,
	}

	if err := fn(ctx, eventTx); err != nil {
		return false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit event transaction: %w", err)
	}

	return true, nil
}

func (t *analyticsEventTx) AccumulateUserStats(
	ctx context.Context,
	userID string,
	totalVolume float64,
	totalReps int,
) error {
	query := `
		INSERT INTO user_stats (
			user_id,
			total_workouts,
			total_volume,
			total_reps,
			avg_intensity,
			updated_at
		)
		VALUES (
			$1,
			1,
			$2,
			$3,
			CASE
				WHEN $3 > 0
				THEN $2 / $3::numeric
				ELSE 0
			END,
			NOW()
		)
		ON CONFLICT (user_id)
		DO UPDATE SET
			total_workouts =
				user_stats.total_workouts + 1,

			total_volume =
				user_stats.total_volume + EXCLUDED.total_volume,

			total_reps =
				user_stats.total_reps + EXCLUDED.total_reps,

			avg_intensity =
				CASE
					WHEN user_stats.total_reps + EXCLUDED.total_reps > 0
					THEN (
						user_stats.total_volume +
						EXCLUDED.total_volume
					) / (
						user_stats.total_reps +
						EXCLUDED.total_reps
					)::numeric
					ELSE 0
				END,

			updated_at = NOW()
	`

	_, err := t.tx.Exec(
		ctx,
		query,
		userID,
		totalVolume,
		totalReps,
	)
	if err != nil {
		return fmt.Errorf(
			"accumulate user stats in event transaction: %w",
			err,
		)
	}

	return nil
}

func (t *analyticsEventTx) MergeExerciseProgress(
	ctx context.Context,
	progress *domain.ExerciseProgress,
) error {
	query := `
		INSERT INTO exercise_progress (
			user_id,
			exercise_id,
			best_weight,
			total_reps,
			last_workout_at,
			estimated_1rm,
			updated_at
		)
		VALUES (
			$1,
			$2,
			$3,
			$4,
			$5,
			$6,
			NOW()
		)
		ON CONFLICT (user_id, exercise_id)
		DO UPDATE SET
			best_weight = GREATEST(
				exercise_progress.best_weight,
				EXCLUDED.best_weight
			),

			total_reps =
				exercise_progress.total_reps +
				EXCLUDED.total_reps,

			last_workout_at = GREATEST(
				COALESCE(
					exercise_progress.last_workout_at,
					EXCLUDED.last_workout_at
				),
				EXCLUDED.last_workout_at
			),

			estimated_1rm = GREATEST(
				exercise_progress.estimated_1rm,
				EXCLUDED.estimated_1rm
			),

			updated_at = NOW()
	`

	_, err := t.tx.Exec(
		ctx,
		query,
		progress.UserID,
		progress.ExerciseID,
		progress.BestWeight,
		progress.TotalReps,
		progress.LastWorkoutAt,
		progress.Estimated1RM,
	)
	if err != nil {
		return fmt.Errorf(
			"merge exercise progress in event transaction: %w",
			err,
		)
	}

	return nil
}

func (t *analyticsEventTx) InsertWorkoutSummary(
	ctx context.Context,
	summary *domain.WorkoutSummary,
) error {
	query := `
		INSERT INTO workout_summary (
			workout_id,
			user_id,
			name,
			date,
			total_volume,
			set_count,
			created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (workout_id) DO NOTHING
	`

	_, err := t.tx.Exec(
		ctx,
		query,
		summary.WorkoutID,
		summary.UserID,
		summary.Name,
		summary.Date,
		summary.TotalVolume,
		summary.SetCount,
	)
	if err != nil {
		return fmt.Errorf(
			"insert workout summary in event transaction: %w",
			err,
		)
	}

	return nil
}

func (r *AnalyticsRepo) operationCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	if r.operationTimeout <= 0 {
		return context.WithCancel(ctx)
	}

	return context.WithTimeout(ctx, r.operationTimeout)
}
