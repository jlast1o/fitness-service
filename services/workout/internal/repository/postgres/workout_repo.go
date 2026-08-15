package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fitness-platform/services/workout/internal/domain"
	"fitness-platform/services/workout/internal/repository"
)

type WorkoutRepo struct {
	pool *pgxpool.Pool
}

func NewWorkoutRepo(pool *pgxpool.Pool) repository.WorkoutRepository {
	return &WorkoutRepo{pool: pool}
}

func (r *WorkoutRepo) CreateWorkout(ctx context.Context, workout *domain.Workout, sets []domain.ExerciseSet) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) // откат, если не закоммитим

	workoutQuery := `
		INSERT INTO workouts (user_id, name, date, notes, program_id, template_id, metrics)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at, updated_at
	`
	err = tx.QueryRow(ctx, workoutQuery,
		workout.UserID,
		workout.Name,
		workout.Date,
		workout.Notes,
		workout.ProgramID,
		workout.TemplateID,
		workout.Metrics,
	).Scan(&workout.ID, &workout.CreatedAt, &workout.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert workout: %w", err)
	}

	// Вставляем подходы батчем
	if len(sets) > 0 {
		batch := &pgx.Batch{}
		setQuery := `
			INSERT INTO exercise_sets (workout_id, exercise_id, order_index, weight, reps, rpe, metrics)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`
		for _, set := range sets {
			batch.Queue(setQuery,
				workout.ID,
				set.ExerciseID,
				set.OrderIndex,
				set.Weight,
				set.Reps,
				set.RPE,
				set.Metrics,
			)
		}
		br := tx.SendBatch(ctx, batch)
		if err := br.Close(); err != nil {
			return fmt.Errorf("insert sets batch: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func (r *WorkoutRepo) GetWorkoutByID(ctx context.Context, workoutID string) (*domain.Workout, []domain.ExerciseSet, error) {
	workoutQuery := `
		SELECT id, user_id, name, date, notes, program_id, template_id, metrics, created_at, updated_at
		FROM workouts
		WHERE id = $1
	`
	w := &domain.Workout{}
	err := r.pool.QueryRow(ctx, workoutQuery, workoutID).Scan(
		&w.ID, &w.UserID, &w.Name, &w.Date, &w.Notes, &w.ProgramID, &w.TemplateID, &w.Metrics, &w.CreatedAt, &w.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("query workout: %w", err)
	}

	setsQuery := `
		SELECT id, workout_id, exercise_id, order_index, weight, reps, rpe, metrics, created_at, updated_at
		FROM exercise_sets
		WHERE workout_id = $1
		ORDER BY order_index
	`
	rows, err := r.pool.Query(ctx, setsQuery, workoutID)
	if err != nil {
		return nil, nil, fmt.Errorf("query sets: %w", err)
	}
	defer rows.Close()

	var sets []domain.ExerciseSet
	for rows.Next() {
		var s domain.ExerciseSet
		if err := rows.Scan(
			&s.ID, &s.WorkoutID, &s.ExerciseID, &s.OrderIndex, &s.Weight, &s.Reps, &s.RPE, &s.Metrics, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, nil, fmt.Errorf("scan set: %w", err)
		}
		sets = append(sets, s)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("rows error: %w", err)
	}

	return w, sets, nil
}

func (r *WorkoutRepo) ListWorkoutsByUser(ctx context.Context, userID string, limit, offset int) ([]domain.Workout, error) {
	query := `
		SELECT id, user_id, name, date, notes, program_id, template_id, metrics, created_at, updated_at
		FROM workouts
		WHERE user_id = $1
		ORDER BY date DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := r.pool.Query(ctx, query, userID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("query workouts: %w", err)
	}
	defer rows.Close()

	var workouts []domain.Workout
	for rows.Next() {
		var w domain.Workout
		if err := rows.Scan(
			&w.ID, &w.UserID, &w.Name, &w.Date, &w.Notes, &w.ProgramID, &w.TemplateID, &w.Metrics, &w.CreatedAt, &w.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan workout: %w", err)
		}
		workouts = append(workouts, w)
	}
	return workouts, rows.Err()
}

func (r *WorkoutRepo) DeleteWorkout(ctx context.Context, workoutID string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM workouts WHERE id = $1`, workoutID)
	if err != nil {
		return fmt.Errorf("delete workout: %w", err)
	}
	return nil
}

func (r *WorkoutRepo) UpdateWorkout(ctx context.Context, workout *domain.Workout) error {
	query := `
		UPDATE workouts
		SET name = $2, date = $3, notes = $4, program_id = $5, template_id = $6, metrics = $7, updated_at = NOW()
		WHERE id = $1
	`
	_, err := r.pool.Exec(ctx, query,
		workout.ID,
		workout.Name,
		workout.Date,
		workout.Notes,
		workout.ProgramID,
		workout.TemplateID,
		workout.Metrics,
	)
	return err
}

func (r *WorkoutRepo) ListExercises(ctx context.Context) ([]domain.Exercise, error) {
	query := `SELECT id, name, muscle_group, category, created_at, updated_at FROM exercises ORDER BY name`
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query exercises: %w", err)
	}
	defer rows.Close()

	var exercises []domain.Exercise
	for rows.Next() {
		var e domain.Exercise
		if err := rows.Scan(&e.ID, &e.Name, &e.MuscleGroup, &e.Category, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan exercise: %w", err)
		}
		exercises = append(exercises, e)
	}
	return exercises, rows.Err()
}

func (r *WorkoutRepo) CreateExercise(ctx context.Context, exercise *domain.Exercise) error {
	query := `
		INSERT INTO exercises (name, muscle_group, category)
		VALUES ($1, $2, $3)
		RETURNING id, created_at, updated_at
	`
	err := r.pool.QueryRow(ctx, query, exercise.Name, exercise.MuscleGroup, exercise.Category).
		Scan(&exercise.ID, &exercise.CreatedAt, &exercise.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert exercise: %w", err)
	}
	return nil
}

func (r *WorkoutRepo) GetExerciseByID(ctx context.Context, exerciseID string) (*domain.Exercise, error) {
	query := `SELECT id, name, muscle_group, category, created_at, updated_at FROM exercises WHERE id = $1`
	e := &domain.Exercise{}
	err := r.pool.QueryRow(ctx, query, exerciseID).Scan(&e.ID, &e.Name, &e.MuscleGroup, &e.Category, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("query exercise: %w", err)
	}
	return e, nil
}
