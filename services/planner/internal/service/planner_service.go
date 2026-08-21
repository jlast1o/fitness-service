package service

import (
	"context"
	"errors"
	"time"

	"fitness-platform/pkg/logger"
	"fitness-platform/services/planner/internal/domain"
	"fitness-platform/services/planner/internal/repository"
)

// Специфические ошибки бизнес-логики.
var (
	ErrInvalidInput = errors.New("invalid input")
)

// PlannerService содержит бизнес-логику планировщика.
type PlannerService struct {
	repo repository.PlannerRepository
}

// NewPlannerService создаёт новый экземпляр PlannerService.
func NewPlannerService(repo repository.PlannerRepository) *PlannerService {
	return &PlannerService{repo: repo}
}

// UpsertProfile создаёт или обновляет тренировочный профиль пользователя.
func (s *PlannerService) UpsertProfile(ctx context.Context, profile *domain.UserProfile) error {
	// Валидация
	if profile.UserID == "" || profile.Goal == "" || profile.ExperienceLevel == "" {
		return ErrInvalidInput
	}
	if profile.DaysPerWeek < 1 || profile.DaysPerWeek > 7 {
		return ErrInvalidInput
	}
	if profile.Goal != "strength" && profile.Goal != "fat_loss" {
		return ErrInvalidInput
	}
	if profile.ExperienceLevel != "beginner" && profile.ExperienceLevel != "intermediate" && profile.ExperienceLevel != "advanced" {
		return ErrInvalidInput
	}

	// Инициализируем пустые map, если nil
	if profile.Injuries == nil {
		profile.Injuries = map[string]any{}
	}
	if profile.Current1RM == nil {
		profile.Current1RM = map[string]float64{}
	}

	if err := s.repo.UpsertUserProfile(ctx, profile); err != nil {
		logger.Log.Error().Err(err).Str("user_id", profile.UserID).Msg("failed to upsert profile")
		return err
	}
	return nil
}

// GetProfile возвращает профиль пользователя.
func (s *PlannerService) GetProfile(ctx context.Context, userID string) (*domain.UserProfile, error) {
	if userID == "" {
		return nil, ErrInvalidInput
	}
	profile, err := s.repo.GetUserProfile(ctx, userID)
	if err != nil {
		logger.Log.Error().Err(err).Str("user_id", userID).Msg("failed to get profile")
		return nil, err
	}
	return profile, nil
}

// ListExercises возвращает все доступные упражнения.
func (s *PlannerService) ListExercises(ctx context.Context) ([]domain.AvailableExercise, error) {
	exercises, err := s.repo.ListAvailableExercises(ctx)
	if err != nil {
		logger.Log.Error().Err(err).Msg("failed to list exercises")
		return nil, err
	}
	return exercises, nil
}

// GetExercise возвращает упражнение по ID.
func (s *PlannerService) GetExercise(ctx context.Context, exerciseID string) (*domain.AvailableExercise, error) {
	if exerciseID == "" {
		return nil, ErrInvalidInput
	}
	exercise, err := s.repo.GetAvailableExerciseByID(ctx, exerciseID)
	if err != nil {
		logger.Log.Error().Err(err).Str("exercise_id", exerciseID).Msg("failed to get exercise")
		return nil, err
	}
	return exercise, nil
}

// ProcessWorkoutCreated обрабатывает событие о выполненной тренировке.
// Адаптирует план на основе фактических RPE и весов.
func (s *PlannerService) ProcessWorkoutCreated(ctx context.Context, event domain.WorkoutCreatedEvent) error {
	// 1. Найти активный план пользователя
	plan, err := s.repo.GetActivePlanByUserID(ctx, event.UserID)
	if err != nil {
		logger.Log.Error().Err(err).Str("user_id", event.UserID).Msg("failed to get active plan")
		return err
	}
	if plan == nil {
		// Нет активного плана — адаптировать нечего
		return nil
	}

	// 2. Получить запланированные упражнения на дату тренировки
	plannedExercises, err := s.repo.GetPlannedExercisesForDate(ctx, event.UserID, event.Date)
	if err != nil {
		logger.Log.Error().Err(err).Msg("failed to get planned exercises for date")
		return err
	}
	if len(plannedExercises) == 0 {
		// Тренировка вне плана — не адаптируем
		return nil
	}

	// 3. Сгруппировать фактические подходы по упражнениям
	type actualSet struct {
		TotalWeight float64
		TotalReps   int
		RPEs        []float64
	}
	actualByExercise := make(map[string]*actualSet)
	for _, set := range event.Sets {
		agg, exists := actualByExercise[set.ExerciseID]
		if !exists {
			agg = &actualSet{}
			actualByExercise[set.ExerciseID] = agg
		}
		agg.TotalWeight += set.Weight * float64(set.Reps)
		agg.TotalReps += set.Reps
		if set.RPE > 0 {
			agg.RPEs = append(agg.RPEs, set.RPE)
		}
	}

	// 4. Для каждого запланированного упражнения найти факт и адаптировать
	for _, planned := range plannedExercises {
		actual, exists := actualByExercise[planned.ExerciseID]
		if !exists || actual.TotalReps == 0 {
			continue // не выполнялось, пропускаем
		}

		// Если есть RPE, используем его для корректировки
		if len(actual.RPEs) > 0 && planned.TargetRPE > 0 {
			avgRPE := average(actual.RPEs)
			// Разница между фактическим и целевым
			delta := avgRPE - planned.TargetRPE

			// Если фактический RPE ниже целевого на 0.5 и более — увеличиваем вес
			if delta < -0.5 {
				planned.TargetWeight = planned.TargetWeight + 2.5
			} else if delta > 0.5 {
				// Если выше — снижаем
				planned.TargetWeight = planned.TargetWeight - 2.5
				if planned.TargetWeight < 0 {
					planned.TargetWeight = 0
				}
			}
			// Обновляем запланированное упражнение
			if err := s.repo.UpdatePlannedExercise(ctx, &planned); err != nil {
				logger.Log.Error().Err(err).Str("exercise_id", planned.ExerciseID).Msg("failed to update planned exercise")
				return err
			}
		} else {
			// Если RPE не указан, можно адаптировать по объёму: если пользователь сделал меньше повторений, чем целевые, снизить вес
			actualAvgWeight := actual.TotalWeight / float64(actual.TotalReps)
			// Если фактический средний вес сильно ниже целевого, снизим целевой
			if planned.TargetWeight > 0 && actualAvgWeight < planned.TargetWeight*0.8 {
				planned.TargetWeight = actualAvgWeight
				if err := s.repo.UpdatePlannedExercise(ctx, &planned); err != nil {
					logger.Log.Error().Err(err).Str("exercise_id", planned.ExerciseID).Msg("failed to update planned exercise weight")
					return err
				}
			}
		}
	}

	return nil
}

// average возвращает среднее значение для среза float64.
func average(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

// CreatePlan сохраняет готовый план и его компоненты.
func (s *PlannerService) CreatePlan(ctx context.Context, plan *domain.TrainingPlan, weeks []domain.PlanWeek, days []domain.PlanDay, exercises []domain.PlannedExercise) error {
	if plan == nil || plan.UserID == "" {
		return ErrInvalidInput
	}
	return s.repo.CreatePlan(ctx, plan, weeks, days, exercises)
}

// GenerateAndSavePlan генерирует план на основе профиля пользователя и сохраняет его.
// Возвращает созданный план.
func (s *PlannerService) GenerateAndSavePlan(ctx context.Context, userID string, startDate time.Time, durationWeeks int) (*domain.TrainingPlan, error) {
	if userID == "" {
		return nil, ErrInvalidInput
	}
	profile, err := s.repo.GetUserProfile(ctx, userID)
	if err != nil {
		logger.Log.Error().Err(err).Str("user_id", userID).Msg("failed to get profile for plan generation")
		return nil, err
	}
	if profile == nil {
		return nil, ErrInvalidInput // или ErrProfileNotFound, но для простоты так
	}

	generator := NewPlanGenerator(s.repo)
	plan, weeks, days, exercises, err := generator.GeneratePlan(ctx, profile, startDate, durationWeeks)
	if err != nil {
		logger.Log.Error().Err(err).Msg("failed to generate plan")
		return nil, err
	}

	if err := s.repo.CreatePlan(ctx, plan, weeks, days, exercises); err != nil {
		logger.Log.Error().Err(err).Msg("failed to save generated plan")
		return nil, err
	}

	return plan, nil
}

// GetActivePlan возвращает активный план пользователя.
func (s *PlannerService) GetActivePlan(ctx context.Context, userID string) (*domain.TrainingPlan, error) {
	if userID == "" {
		return nil, ErrInvalidInput
	}
	plan, err := s.repo.GetActivePlanByUserID(ctx, userID)
	if err != nil {
		logger.Log.Error().Err(err).Str("user_id", userID).Msg("failed to get active plan")
		return nil, err
	}
	return plan, nil
}

// GetNextWorkout возвращает ближайший запланированный день и его упражнения.
func (s *PlannerService) GetNextWorkout(ctx context.Context, userID string) (*domain.PlanDay, []domain.PlannedExercise, error) {
	if userID == "" {
		return nil, nil, ErrInvalidInput
	}
	day, exercises, err := s.repo.GetNextPlannedDay(ctx, userID, time.Now())
	if err != nil {
		logger.Log.Error().Err(err).Str("user_id", userID).Msg("failed to get next workout")
		return nil, nil, err
	}
	return day, exercises, nil
}
