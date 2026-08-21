package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fitness-platform/services/planner/internal/domain"
	"fitness-platform/services/planner/internal/repository"
)

// PlannerRepo реализует интерфейс repository.PlannerRepository.
type PlannerRepo struct {
	pool *pgxpool.Pool
}

// NewPlannerRepo создаёт новый экземпляр PlannerRepo.
func NewPlannerRepo(pool *pgxpool.Pool) repository.PlannerRepository {
	return &PlannerRepo{pool: pool}
}

// UpsertUserProfile вставляет или обновляет тренировочный профиль пользователя.
func (r *PlannerRepo) UpsertUserProfile(ctx context.Context, profile *domain.UserProfile) error {
	if profile.Injuries == nil {
		profile.Injuries = map[string]any{}
	}
	if profile.Current1RM == nil {
		profile.Current1RM = map[string]float64{}
	}

	query := `
		INSERT INTO user_profiles (user_id, goal, experience_level, days_per_week, injuries, current_1rm, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (user_id)
		DO UPDATE SET
			goal = EXCLUDED.goal,
			experience_level = EXCLUDED.experience_level,
			days_per_week = EXCLUDED.days_per_week,
			injuries = EXCLUDED.injuries,
			current_1rm = EXCLUDED.current_1rm,
			updated_at = NOW()
	`
	_, err := r.pool.Exec(ctx, query,
		profile.UserID,
		profile.Goal,
		profile.ExperienceLevel,
		profile.DaysPerWeek,
		profile.Injuries,
		profile.Current1RM,
	)
	if err != nil {
		return fmt.Errorf("upsert user profile: %w", err)
	}
	return nil
}

// GetUserProfile возвращает профиль пользователя по ID.
func (r *PlannerRepo) GetUserProfile(ctx context.Context, userID string) (*domain.UserProfile, error) {
	query := `
		SELECT user_id, goal, experience_level, days_per_week, injuries, current_1rm, created_at, updated_at
		FROM user_profiles
		WHERE user_id = $1
	`
	profile := &domain.UserProfile{}
	err := r.pool.QueryRow(ctx, query, userID).Scan(
		&profile.UserID,
		&profile.Goal,
		&profile.ExperienceLevel,
		&profile.DaysPerWeek,
		&profile.Injuries,
		&profile.Current1RM,
		&profile.CreatedAt,
		&profile.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get user profile: %w", err)
	}
	return profile, nil
}

// ListAvailableExercises возвращает все упражнения из справочника Planner.
func (r *PlannerRepo) ListAvailableExercises(ctx context.Context) ([]domain.AvailableExercise, error) {
	query := `
		SELECT id, name, muscle_group, category, created_at, updated_at
		FROM available_exercises
		ORDER BY name
	`
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list available exercises: %w", err)
	}
	defer rows.Close()

	var exercises []domain.AvailableExercise
	for rows.Next() {
		var e domain.AvailableExercise
		if err := rows.Scan(&e.ID, &e.Name, &e.MuscleGroup, &e.Category, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan available exercise: %w", err)
		}
		exercises = append(exercises, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return exercises, nil
}

// GetAvailableExerciseByID возвращает упражнение по ID.
func (r *PlannerRepo) GetAvailableExerciseByID(ctx context.Context, exerciseID string) (*domain.AvailableExercise, error) {
	query := `
		SELECT id, name, muscle_group, category, created_at, updated_at
		FROM available_exercises
		WHERE id = $1
	`
	exercise := &domain.AvailableExercise{}
	err := r.pool.QueryRow(ctx, query, exerciseID).Scan(
		&exercise.ID,
		&exercise.Name,
		&exercise.MuscleGroup,
		&exercise.Category,
		&exercise.CreatedAt,
		&exercise.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get available exercise by id: %w", err)
	}
	return exercise, nil
}

// CreatePlan создаёт план вместе с неделями, днями и упражнениями в одной транзакции.
func (r *PlannerRepo) CreatePlan(
	ctx context.Context,
	plan *domain.TrainingPlan,
	weeks []domain.PlanWeek,
	days []domain.PlanDay,
	exercises []domain.PlannedExercise,
) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Вставляем план (здесь ID по-прежнему генерируется базой)
	planQuery := `
		INSERT INTO training_plans (user_id, name, goal, experience_level, start_date, end_date, status, progression_rule)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at, updated_at
	`
	err = tx.QueryRow(ctx, planQuery,
		plan.UserID, plan.Name, plan.Goal, plan.ExperienceLevel,
		plan.StartDate, plan.EndDate, plan.Status, plan.ProgressionRule,
	).Scan(&plan.ID, &plan.CreatedAt, &plan.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert training plan: %w", err)
	}

	// Вставляем недели с уже сгенерированными ID
	for _, week := range weeks {
		week.PlanID = plan.ID
		query := `
			INSERT INTO plan_weeks (id, plan_id, week_number, focus)
			VALUES ($1, $2, $3, $4)
		`
		if _, err := tx.Exec(ctx, query, week.ID, week.PlanID, week.WeekNumber, week.Focus); err != nil {
			return fmt.Errorf("insert plan week: %w", err)
		}
	}

	// Вставляем дни с уже сгенерированными ID и WeekID
	for _, day := range days {
		query := `
			INSERT INTO plan_days (id, week_id, day_number, date, name)
			VALUES ($1, $2, $3, $4, $5)
		`
		if _, err := tx.Exec(ctx, query, day.ID, day.WeekID, day.DayNumber, day.Date, day.Name); err != nil {
			return fmt.Errorf("insert plan day: %w", err)
		}
	}

	// Вставляем запланированные упражнения с уже сгенерированными ID и DayID
	for _, exercise := range exercises {
		query := `
			INSERT INTO planned_exercises (id, day_id, exercise_id, target_sets, target_reps_min, target_reps_max, target_weight, target_rpe, notes, order_index)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		`
		if _, err := tx.Exec(ctx, query,
			exercise.ID,
			exercise.DayID,
			exercise.ExerciseID,
			exercise.TargetSets,
			exercise.TargetRepsMin,
			exercise.TargetRepsMax,
			exercise.TargetWeight,
			exercise.TargetRPE,
			exercise.Notes,
			exercise.OrderIndex,
		); err != nil {
			return fmt.Errorf("insert planned exercise: %w", err)
		}
	}

	return tx.Commit(ctx)
}

// GetPlanByID возвращает план по ID.
func (r *PlannerRepo) GetPlanByID(ctx context.Context, planID string) (*domain.TrainingPlan, error) {
	query := `
		SELECT id, user_id, name, goal, experience_level, start_date, end_date, status, progression_rule, created_at, updated_at
		FROM training_plans
		WHERE id = $1
	`
	plan := &domain.TrainingPlan{}
	err := r.pool.QueryRow(ctx, query, planID).Scan(
		&plan.ID,
		&plan.UserID,
		&plan.Name,
		&plan.Goal,
		&plan.ExperienceLevel,
		&plan.StartDate,
		&plan.EndDate,
		&plan.Status,
		&plan.ProgressionRule,
		&plan.CreatedAt,
		&plan.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get plan by id: %w", err)
	}
	return plan, nil
}

// GetActivePlanByUserID возвращает активный план пользователя.
func (r *PlannerRepo) GetActivePlanByUserID(ctx context.Context, userID string) (*domain.TrainingPlan, error) {
	query := `
		SELECT id, user_id, name, goal, experience_level, start_date, end_date, status, progression_rule, created_at, updated_at
		FROM training_plans
		WHERE user_id = $1 AND status = 'active'
		ORDER BY created_at DESC
		LIMIT 1
	`
	plan := &domain.TrainingPlan{}
	err := r.pool.QueryRow(ctx, query, userID).Scan(
		&plan.ID,
		&plan.UserID,
		&plan.Name,
		&plan.Goal,
		&plan.ExperienceLevel,
		&plan.StartDate,
		&plan.EndDate,
		&plan.Status,
		&plan.ProgressionRule,
		&plan.CreatedAt,
		&plan.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get active plan: %w", err)
	}
	return plan, nil
}

// ListPlansByUserID возвращает все планы пользователя.
func (r *PlannerRepo) ListPlansByUserID(ctx context.Context, userID string) ([]domain.TrainingPlan, error) {
	query := `
		SELECT id, user_id, name, goal, experience_level, start_date, end_date, status, progression_rule, created_at, updated_at
		FROM training_plans
		WHERE user_id = $1
		ORDER BY created_at DESC
	`
	rows, err := r.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("list plans: %w", err)
	}
	defer rows.Close()

	var plans []domain.TrainingPlan
	for rows.Next() {
		var p domain.TrainingPlan
		if err := rows.Scan(
			&p.ID, &p.UserID, &p.Name, &p.Goal, &p.ExperienceLevel,
			&p.StartDate, &p.EndDate, &p.Status, &p.ProgressionRule,
			&p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan plan: %w", err)
		}
		plans = append(plans, p)
	}
	return plans, rows.Err()
}

// UpdatePlanStatus обновляет статус плана.
func (r *PlannerRepo) UpdatePlanStatus(ctx context.Context, planID, status string) error {
	_, err := r.pool.Exec(ctx, `UPDATE training_plans SET status = $2, updated_at = NOW() WHERE id = $1`, planID, status)
	if err != nil {
		return fmt.Errorf("update plan status: %w", err)
	}
	return nil
}

// GetPlanWeeks возвращает недели плана по planID.
func (r *PlannerRepo) GetPlanWeeks(ctx context.Context, planID string) ([]domain.PlanWeek, error) {
	query := `
		SELECT id, plan_id, week_number, focus, created_at
		FROM plan_weeks
		WHERE plan_id = $1
		ORDER BY week_number
	`
	rows, err := r.pool.Query(ctx, query, planID)
	if err != nil {
		return nil, fmt.Errorf("list plan weeks: %w", err)
	}
	defer rows.Close()

	var weeks []domain.PlanWeek
	for rows.Next() {
		var w domain.PlanWeek
		if err := rows.Scan(&w.ID, &w.PlanID, &w.WeekNumber, &w.Focus, &w.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan plan week: %w", err)
		}
		weeks = append(weeks, w)
	}
	return weeks, rows.Err()
}

// GetPlanDays возвращает дни недели по weekID.
func (r *PlannerRepo) GetPlanDays(ctx context.Context, weekID string) ([]domain.PlanDay, error) {
	query := `
		SELECT id, week_id, day_number, date, name, created_at
		FROM plan_days
		WHERE week_id = $1
		ORDER BY day_number
	`
	rows, err := r.pool.Query(ctx, query, weekID)
	if err != nil {
		return nil, fmt.Errorf("list plan days: %w", err)
	}
	defer rows.Close()

	var days []domain.PlanDay
	for rows.Next() {
		var d domain.PlanDay
		if err := rows.Scan(&d.ID, &d.WeekID, &d.DayNumber, &d.Date, &d.Name, &d.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan plan day: %w", err)
		}
		days = append(days, d)
	}
	return days, rows.Err()
}

// GetPlannedExercises возвращает запланированные упражнения для дня.
func (r *PlannerRepo) GetPlannedExercises(ctx context.Context, dayID string) ([]domain.PlannedExercise, error) {
	query := `
		SELECT id, day_id, exercise_id, target_sets, target_reps_min, target_reps_max,
		       target_weight, target_rpe, notes, order_index, created_at, updated_at
		FROM planned_exercises
		WHERE day_id = $1
		ORDER BY order_index
	`
	rows, err := r.pool.Query(ctx, query, dayID)
	if err != nil {
		return nil, fmt.Errorf("list planned exercises: %w", err)
	}
	defer rows.Close()

	var exercises []domain.PlannedExercise
	for rows.Next() {
		var e domain.PlannedExercise
		if err := rows.Scan(
			&e.ID, &e.DayID, &e.ExerciseID, &e.TargetSets, &e.TargetRepsMin,
			&e.TargetRepsMax, &e.TargetWeight, &e.TargetRPE, &e.Notes,
			&e.OrderIndex, &e.CreatedAt, &e.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan planned exercise: %w", err)
		}
		exercises = append(exercises, e)
	}
	return exercises, rows.Err()
}

// GetNextPlannedDay возвращает ближайший запланированный день и его упражнения.
// fromDate — дата, от которой ищем (обычно текущая).
func (r *PlannerRepo) GetNextPlannedDay(ctx context.Context, userID string, fromDate time.Time) (*domain.PlanDay, []domain.PlannedExercise, error) {
	// Находим ближайший день для активного плана пользователя, начиная с fromDate
	dayQuery := `
		SELECT pd.id, pd.week_id, pd.day_number, pd.date, pd.name, pd.created_at
		FROM plan_days pd
		JOIN plan_weeks pw ON pd.week_id = pw.id
		JOIN training_plans tp ON pw.plan_id = tp.id
		WHERE tp.user_id = $1 AND tp.status = 'active' AND pd.date >= $2
		ORDER BY pd.date
		LIMIT 1
	`
	day := &domain.PlanDay{}
	err := r.pool.QueryRow(ctx, dayQuery, userID, fromDate).Scan(
		&day.ID, &day.WeekID, &day.DayNumber, &day.Date, &day.Name, &day.CreatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("get next planned day: %w", err)
	}

	// Получаем упражнения для этого дня
	exercises, err := r.GetPlannedExercises(ctx, day.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("get exercises for next day: %w", err)
	}

	return day, exercises, nil
}

// GetPlannedExercisesForDate возвращает запланированные упражнения на конкретную дату.
// Используется при адаптации: по дате фактической тренировки находим план.
func (r *PlannerRepo) GetPlannedExercisesForDate(ctx context.Context, userID string, date time.Time) ([]domain.PlannedExercise, error) {
	// Находим день плана на эту дату
	dayQuery := `
		SELECT pd.id
		FROM plan_days pd
		JOIN plan_weeks pw ON pd.week_id = pw.id
		JOIN training_plans tp ON pw.plan_id = tp.id
		WHERE tp.user_id = $1 AND tp.status = 'active' AND pd.date = $2
		LIMIT 1
	`
	var dayID string
	err := r.pool.QueryRow(ctx, dayQuery, userID, date).Scan(&dayID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil // нет запланированного дня на эту дату
		}
		return nil, fmt.Errorf("find planned day by date: %w", err)
	}

	return r.GetPlannedExercises(ctx, dayID)
}

// UpdatePlannedExercise обновляет параметры запланированного упражнения.
func (r *PlannerRepo) UpdatePlannedExercise(ctx context.Context, exercise *domain.PlannedExercise) error {
	query := `
		UPDATE planned_exercises
		SET target_sets = $2,
		    target_reps_min = $3,
		    target_reps_max = $4,
		    target_weight = $5,
		    target_rpe = $6,
		    notes = $7,
		    order_index = $8,
		    updated_at = NOW()
		WHERE id = $1
	`
	_, err := r.pool.Exec(ctx, query,
		exercise.ID,
		exercise.TargetSets,
		exercise.TargetRepsMin,
		exercise.TargetRepsMax,
		exercise.TargetWeight,
		exercise.TargetRPE,
		exercise.Notes,
		exercise.OrderIndex,
	)
	if err != nil {
		return fmt.Errorf("update planned exercise: %w", err)
	}
	return nil
}
