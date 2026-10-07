package repository

import (
	"context"
	"time"

	"fitness-platform/services/planner/internal/domain"
)

// PlannerEventTx описывает операции,
// доступные внутри транзакции обработки события.
type PlannerEventTx interface {
	GetActivePlanByUserID(
		ctx context.Context,
		userID string,
	) (*domain.TrainingPlan, error)

	GetPlannedExercisesForDate(
		ctx context.Context,
		userID string,
		date time.Time,
	) ([]domain.PlannedExercise, error)

	UpdatePlannedExercise(
		ctx context.Context,
		exercise *domain.PlannedExercise,
	) error
}

// PlannerRepository определяет контракт для работы с данными планировщика.
type PlannerRepository interface {
	// Профиль пользователя
	UpsertUserProfile(ctx context.Context, profile *domain.UserProfile) error
	GetUserProfile(ctx context.Context, userID string) (*domain.UserProfile, error)

	// Справочник упражнений
	ListAvailableExercises(ctx context.Context) ([]domain.AvailableExercise, error)
	GetAvailableExerciseByID(ctx context.Context, exerciseID string) (*domain.AvailableExercise, error)

	// Планы и их структура
	CreatePlan(
		ctx context.Context,
		plan *domain.TrainingPlan,
		weeks []domain.PlanWeek,
		days []domain.PlanDay,
		exercises []domain.PlannedExercise,
	) error

	GetPlanByID(
		ctx context.Context,
		planID string,
	) (*domain.TrainingPlan, error)

	GetActivePlanByUserID(
		ctx context.Context,
		userID string,
	) (*domain.TrainingPlan, error)

	UpdatePlanStatus(
		ctx context.Context,
		planID,
		status string,
	) error

	ListPlansByUserID(
		ctx context.Context,
		userID string,
	) ([]domain.TrainingPlan, error)

	// Детали плана
	GetPlanWeeks(
		ctx context.Context,
		planID string,
	) ([]domain.PlanWeek, error)

	GetPlanDays(
		ctx context.Context,
		weekID string,
	) ([]domain.PlanDay, error)

	GetPlannedExercises(
		ctx context.Context,
		dayID string,
	) ([]domain.PlannedExercise, error)

	// Адаптация
	UpdatePlannedExercise(
		ctx context.Context,
		exercise *domain.PlannedExercise,
	) error

	// Получение следующего запланированного дня.
	GetNextPlannedDay(
		ctx context.Context,
		userID string,
		fromDate time.Time,
	) (*domain.PlanDay, []domain.PlannedExercise, error)

	// Получение упражнений на конкретную дату.
	GetPlannedExercisesForDate(
		ctx context.Context,
		userID string,
		date time.Time,
	) ([]domain.PlannedExercise, error)

	// Напоминания.
	GetUpcomingWorkouts(
		ctx context.Context,
		from,
		to time.Time,
	) ([]domain.WorkoutReminder, error)

	ReplaceActivePlan(
		ctx context.Context,
		plan *domain.TrainingPlan,
		weeks []domain.PlanWeek,
		days []domain.PlanDay,
		exercises []domain.PlannedExercise,
	) error

	// WithEventTransaction атомарно:
	// 1. пытается зарегистрировать eventID в processed_events;
	// 2. если событие новое — выполняет fn;
	// 3. коммитит claim события и изменения плана вместе.
	//
	// applied == false означает, что eventID уже был обработан.
	WithEventTransaction(
		ctx context.Context,
		eventID string,
		fn func(context.Context, PlannerEventTx) error,
	) (applied bool, err error)
}
