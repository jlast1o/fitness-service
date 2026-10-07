package service

import (
	"context"
	"errors"
	"time"

	"fitness-platform/pkg/logger"
	"fitness-platform/services/planner/internal/domain"
	"fitness-platform/services/planner/internal/repository"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

// Специфические ошибки бизнес-логики.
var (
	ErrInvalidInput = errors.New("invalid input")
)

// PlannerService содержит бизнес-логику планировщика.
type PlannerService struct {
	repo   repository.PlannerRepository
	tracer trace.Tracer
}

// NewPlannerService создаёт новый экземпляр PlannerService.
func NewPlannerService(repo repository.PlannerRepository) *PlannerService {
	return &PlannerService{
		repo:   repo,
		tracer: otel.Tracer("planner.service")}
}

// UpsertProfile создаёт или обновляет тренировочный профиль пользователя.
func (s *PlannerService) UpsertProfile(ctx context.Context, profile *domain.UserProfile) error {
	ctx, span := s.tracer.Start(
		ctx,
		"PlannerService.UpsertProfile",
	)
	defer span.End()

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
		logger.FromContext(ctx).Error().Err(err).Str("user_id", profile.UserID).Msg("failed to upsert profile")
		return err
	}
	return nil
}

// GetProfile возвращает профиль пользователя.
func (s *PlannerService) GetProfile(ctx context.Context, userID string) (*domain.UserProfile, error) {
	ctx, span := s.tracer.Start(
		ctx,
		"PlannerService.GetProfile",
	)
	defer span.End()

	if userID == "" {
		return nil, ErrInvalidInput
	}
	profile, err := s.repo.GetUserProfile(ctx, userID)
	if err != nil {
		logger.FromContext(ctx).Error().Err(err).Str("user_id", userID).Msg("failed to get profile")
		return nil, err
	}
	return profile, nil
}

// ListExercises возвращает все доступные упражнения.
func (s *PlannerService) ListExercises(ctx context.Context) ([]domain.AvailableExercise, error) {
	ctx, span := s.tracer.Start(
		ctx,
		"PlannerService.ListExercises",
	)
	defer span.End()

	exercises, err := s.repo.ListAvailableExercises(ctx)
	if err != nil {
		logger.FromContext(ctx).Error().Err(err).Msg("failed to list exercises")
		return nil, err
	}
	return exercises, nil
}

// GetExercise возвращает упражнение по ID.
func (s *PlannerService) GetExercise(ctx context.Context, exerciseID string) (*domain.AvailableExercise, error) {
	ctx, span := s.tracer.Start(
		ctx,
		"PlannerService.GetExercise",
	)
	defer span.End()

	if exerciseID == "" {
		return nil, ErrInvalidInput
	}
	exercise, err := s.repo.GetAvailableExerciseByID(ctx, exerciseID)
	if err != nil {
		logger.FromContext(ctx).Error().Err(err).Str("exercise_id", exerciseID).Msg("failed to get exercise")
		return nil, err
	}
	return exercise, nil
}

// ProcessWorkoutCreated обрабатывает событие о выполненной тренировке.
// Повторная доставка одного event_id не должна повторно адаптировать план.
func (s *PlannerService) ProcessWorkoutCreated(
	ctx context.Context,
	eventID string,
	event domain.WorkoutCreatedEvent,
) error {
	ctx, span := s.tracer.Start(
		ctx,
		"PlannerService.ProcessWorkoutCreated",
	)
	defer span.End()

	// Сначала выполняем чистые вычисления вне PostgreSQL-транзакции,
	// чтобы не держать транзакцию открытой дольше необходимого.
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
			agg.RPEs = append(
				agg.RPEs,
				set.RPE,
			)
		}
	}

	applied, err := s.repo.WithEventTransaction(
		ctx,
		eventID,
		func(
			ctx context.Context,
			tx repository.PlannerEventTx,
		) error {
			// 1. Ищем активный план внутри той же транзакции.
			plan, err := tx.GetActivePlanByUserID(
				ctx,
				event.UserID,
			)
			if err != nil {
				return err
			}

			if plan == nil {
				// Активного плана нет.
				// Событие всё равно считается успешно обработанным.
				return nil
			}

			// 2. Получаем упражнения на дату тренировки
			// через ту же PostgreSQL-транзакцию.
			plannedExercises, err := tx.GetPlannedExercisesForDate(
				ctx,
				event.UserID,
				event.Date,
			)
			if err != nil {
				return err
			}

			if len(plannedExercises) == 0 {
				// Тренировка не относится к активному плану.
				return nil
			}

			// 3. Адаптируем упражнения.
			for _, planned := range plannedExercises {
				actual, exists := actualByExercise[planned.ExerciseID]
				if !exists || actual.TotalReps == 0 {
					continue
				}

				if len(actual.RPEs) > 0 && planned.TargetRPE > 0 {
					avgRPE := average(actual.RPEs)
					delta := avgRPE - planned.TargetRPE

					if delta < -0.5 {
						planned.TargetWeight += 2.5
					} else if delta > 0.5 {
						planned.TargetWeight -= 2.5

						if planned.TargetWeight < 0 {
							planned.TargetWeight = 0
						}
					}

					if err := tx.UpdatePlannedExercise(
						ctx,
						&planned,
					); err != nil {
						return err
					}

					continue
				}

				// Если RPE не указан, адаптируем по среднему фактическому весу.
				actualAvgWeight :=
					actual.TotalWeight / float64(actual.TotalReps)

				if planned.TargetWeight > 0 &&
					actualAvgWeight < planned.TargetWeight*0.8 {

					planned.TargetWeight = actualAvgWeight

					if err := tx.UpdatePlannedExercise(
						ctx,
						&planned,
					); err != nil {
						return err
					}
				}
			}

			return nil
		},
	)
	if err != nil {
		logger.FromContext(ctx).Error().
			Err(err).
			Str("event_id", eventID).
			Str("workout_id", event.WorkoutID).
			Str("user_id", event.UserID).
			Msg("failed to process workout.created event")

		return err
	}

	if !applied {
		logger.FromContext(ctx).Debug().
			Str("event_id", eventID).
			Str("workout_id", event.WorkoutID).
			Msg("event already processed, skipping")

		return nil
	}

	logger.FromContext(ctx).Debug().
		Str("event_id", eventID).
		Str("workout_id", event.WorkoutID).
		Msg("workout.created event processed")

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
	ctx, span := s.tracer.Start(
		ctx,
		"PlannerService.CreatePlan",
	)
	defer span.End()

	if plan == nil || plan.UserID == "" {
		return ErrInvalidInput
	}
	return s.repo.CreatePlan(ctx, plan, weeks, days, exercises)
}

// GenerateAndSavePlan генерирует план на основе профиля пользователя и сохраняет его.
// Возвращает созданный план.
func (s *PlannerService) GenerateAndSavePlan(ctx context.Context, userID string, startDate time.Time, durationWeeks int) (*domain.TrainingPlan, error) {
	ctx, span := s.tracer.Start(
		ctx,
		"PlannerService.GenerateAndSavePlan",
	)
	defer span.End()

	if userID == "" {
		return nil, ErrInvalidInput
	}
	profile, err := s.repo.GetUserProfile(ctx, userID)
	if err != nil {
		logger.FromContext(ctx).Error().Err(err).Str("user_id", userID).Msg("failed to get profile for plan generation")
		return nil, err
	}
	if profile == nil {
		return nil, ErrInvalidInput
	}
	generator := NewPlanGenerator(s.repo)
	plan, weeks, days, exercises, err := generator.GeneratePlan(ctx, profile, startDate, durationWeeks)
	if err != nil {
		logger.FromContext(ctx).Error().Err(err).Msg("failed to generate plan")
		return nil, err
	}

	if err := s.repo.ReplaceActivePlan(
		ctx,
		plan,
		weeks,
		days,
		exercises,
	); err != nil {
		logger.FromContext(ctx).Error().
			Err(err).
			Str("user_id", userID).
			Msg("failed to replace active plan")

		return nil, err
	}

	return plan, nil
}

// GetActivePlan возвращает активный план пользователя.
func (s *PlannerService) GetActivePlan(ctx context.Context, userID string) (*domain.TrainingPlan, error) {
	ctx, span := s.tracer.Start(
		ctx,
		"PlannerService.GetActivePlan",
	)
	defer span.End()

	if userID == "" {
		return nil, ErrInvalidInput
	}
	plan, err := s.repo.GetActivePlanByUserID(ctx, userID)
	if err != nil {
		logger.FromContext(ctx).Error().Err(err).Str("user_id", userID).Msg("failed to get active plan")
		return nil, err
	}
	return plan, nil
}

// GetNextWorkout возвращает ближайший запланированный день и его упражнения.
func (s *PlannerService) GetNextWorkout(ctx context.Context, userID string) (*domain.PlanDay, []domain.PlannedExercise, error) {
	ctx, span := s.tracer.Start(
		ctx,
		"PlannerService.GetNextWorkout",
	)
	defer span.End()

	if userID == "" {
		return nil, nil, ErrInvalidInput
	}
	day, exercises, err := s.repo.GetNextPlannedDay(ctx, userID, time.Now())
	if err != nil {
		logger.FromContext(ctx).Error().Err(err).Str("user_id", userID).Msg("failed to get next workout")
		return nil, nil, err
	}
	return day, exercises, nil
}

// GetUpcomingWorkouts возвращает предстоящие тренировки для напоминаний.
func (s *PlannerService) GetUpcomingWorkouts(ctx context.Context, from, to time.Time) ([]domain.WorkoutReminder, error) {
	ctx, span := s.tracer.Start(
		ctx,
		"PlannerService.GetUpcomingWorkouts",
	)
	defer span.End()

	if from.After(to) {
		return nil, ErrInvalidInput
	}
	return s.repo.GetUpcomingWorkouts(ctx, from, to)
}
