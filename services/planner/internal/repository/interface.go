package repository

import (
	"context"
	"time"

	"fitness-platform/services/planner/internal/domain"
)

// PlannerRepository определяет контракт для работы с данными планировщика.
type PlannerRepository interface {
	// Профиль пользователя
	UpsertUserProfile(ctx context.Context, profile *domain.UserProfile) error
	GetUserProfile(ctx context.Context, userID string) (*domain.UserProfile, error)

	// Справочник упражнений
	ListAvailableExercises(ctx context.Context) ([]domain.AvailableExercise, error)
	GetAvailableExerciseByID(ctx context.Context, exerciseID string) (*domain.AvailableExercise, error)

	// Планы и их структура
	CreatePlan(ctx context.Context, plan *domain.TrainingPlan, weeks []domain.PlanWeek, days []domain.PlanDay, exercises []domain.PlannedExercise) error
	GetPlanByID(ctx context.Context, planID string) (*domain.TrainingPlan, error)
	GetActivePlanByUserID(ctx context.Context, userID string) (*domain.TrainingPlan, error)
	UpdatePlanStatus(ctx context.Context, planID, status string) error
	ListPlansByUserID(ctx context.Context, userID string) ([]domain.TrainingPlan, error)

	// Детали плана
	GetPlanWeeks(ctx context.Context, planID string) ([]domain.PlanWeek, error)
	GetPlanDays(ctx context.Context, weekID string) ([]domain.PlanDay, error)
	GetPlannedExercises(ctx context.Context, dayID string) ([]domain.PlannedExercise, error)

	// Адаптация
	UpdatePlannedExercise(ctx context.Context, exercise *domain.PlannedExercise) error

	// Получение следующего запланированного дня (ближайшая тренировка)
	GetNextPlannedDay(ctx context.Context, userID string, fromDate time.Time) (*domain.PlanDay, []domain.PlannedExercise, error)

	// Получение запланированных упражнений на конкретную дату (для адаптации)
	GetPlannedExercisesForDate(ctx context.Context, userID string, date time.Time) ([]domain.PlannedExercise, error)
}
