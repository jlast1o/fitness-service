package service

import (
	"context"
	"time"

	"fitness-platform/pkg/logger"
	"fitness-platform/services/analytics/internal/domain"
	"fitness-platform/services/analytics/internal/repository"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

// AnalyticsService содержит бизнес-логику аналитики.
type AnalyticsService struct {
	repo   repository.AnalyticsRepository
	tracer trace.Tracer
}

// NewAnalyticsService создаёт новый экземпляр AnalyticsService.
func NewAnalyticsService(repo repository.AnalyticsRepository) *AnalyticsService {
	return &AnalyticsService{repo: repo, tracer: otel.Tracer("analytics.service")}
}

// ProcessWorkoutCreated обрабатывает событие о новой тренировке.
func (s *AnalyticsService) ProcessWorkoutCreated(
	ctx context.Context,
	eventID string,
	event domain.WorkoutCreatedEvent,
) error {
	ctx, span := s.tracer.Start(
		ctx,
		"AnalyticsService.ProcessWorkoutCreated",
	)
	defer span.End()

	// Сначала выполняем вычисления в памяти.
	// Держать PostgreSQL-транзакцию во время этих расчётов нет необходимости.
	totalVolume := 0.0
	totalRepsAll := 0

	exerciseAgg := make(map[string]*struct {
		BestWeight float64
		TotalReps  int
		LastDate   time.Time
		Max1RM     float64
	})

	for _, set := range event.Sets {
		volume := set.Weight * float64(set.Reps)

		totalVolume += volume
		totalRepsAll += set.Reps

		agg, exists := exerciseAgg[set.ExerciseID]
		if !exists {
			agg = &struct {
				BestWeight float64
				TotalReps  int
				LastDate   time.Time
				Max1RM     float64
			}{}

			exerciseAgg[set.ExerciseID] = agg
		}

		if set.Weight > agg.BestWeight {
			agg.BestWeight = set.Weight
		}

		agg.TotalReps += set.Reps

		if event.Date.After(agg.LastDate) {
			agg.LastDate = event.Date
		}

		oneRM := calculate1RM(set.Weight, set.Reps)

		if oneRM > agg.Max1RM {
			agg.Max1RM = oneRM
		}
	}

	applied, err := s.repo.WithEventTransaction(
		ctx,
		eventID,
		func(
			ctx context.Context,
			tx repository.AnalyticsEventTx,
		) error {
			// Обновляем агрегированную статистику пользователя.
			stats, err := tx.GetUserStats(ctx, event.UserID)
			if err != nil {
				return err
			}

			if stats == nil {
				stats = &domain.UserStats{
					UserID: event.UserID,
				}
			}

			stats.TotalWorkouts++
			stats.TotalVolume += totalVolume

			if stats.TotalVolume > 0 && totalRepsAll > 0 {
				stats.AvgIntensity =
					stats.TotalVolume / float64(totalRepsAll)
			}

			if err := tx.UpsertUserStats(ctx, stats); err != nil {
				return err
			}

			// Обновляем прогресс по каждому упражнению.
			for exerciseID, agg := range exerciseAgg {
				progress := &domain.ExerciseProgress{
					UserID:        event.UserID,
					ExerciseID:    exerciseID,
					BestWeight:    agg.BestWeight,
					TotalReps:     agg.TotalReps,
					LastWorkoutAt: agg.LastDate,
					Estimated1RM:  agg.Max1RM,
				}

				if err := tx.UpsertExerciseProgress(
					ctx,
					progress,
				); err != nil {
					return err
				}
			}

			// Сохраняем сводку тренировки.
			summary := &domain.WorkoutSummary{
				WorkoutID:   event.WorkoutID,
				UserID:      event.UserID,
				Name:        event.Name,
				Date:        event.Date,
				TotalVolume: totalVolume,
				SetCount:    event.SetsCount,
			}

			if err := tx.InsertWorkoutSummary(
				ctx,
				summary,
			); err != nil {
				return err
			}

			return nil
		},
	)
	if err != nil {
		logger.FromContext(ctx).
			Error().
			Err(err).
			Str("event_id", eventID).
			Str("workout_id", event.WorkoutID).
			Msg("failed to process workout created event")

		return err
	}

	if !applied {
		logger.FromContext(ctx).
			Info().
			Str("event_id", eventID).
			Str("workout_id", event.WorkoutID).
			Msg("event already processed, skipping")

		return nil
	}

	logger.FromContext(ctx).
		Debug().
		Str("event_id", eventID).
		Str("workout_id", event.WorkoutID).
		Msg("workout created event processed")

	return nil
}

// calculate1RM вычисляет одноповторный максимум по формуле Эпли.
func calculate1RM(weight float64, reps int) float64 {
	if reps <= 0 {
		return 0
	}
	if reps == 1 {
		return weight
	}
	return weight * (1 + float64(reps)/30.0)
}

// GetUserStats возвращает агрегированную статистику пользователя.
func (s *AnalyticsService) GetUserStats(ctx context.Context, userID string) (*domain.UserStats, error) {
	ctx, span := s.tracer.Start(ctx, "AnalyticsService.GetUserStats")
	defer span.End()

	return s.repo.GetUserStats(ctx, userID)
}

// GetExerciseProgress возвращает прогресс по конкретному упражнению.
func (s *AnalyticsService) GetExerciseProgress(ctx context.Context, userID, exerciseID string) (*domain.ExerciseProgress, error) {
	ctx, span := s.tracer.Start(ctx, "AnalyticsService.GetExerciseProgress")
	defer span.End()

	return s.repo.GetExerciseProgress(ctx, userID, exerciseID)
}

// ListExerciseProgress возвращает прогресс по всем упражнениям пользователя.
func (s *AnalyticsService) ListExerciseProgress(ctx context.Context, userID string) ([]domain.ExerciseProgress, error) {
	ctx, span := s.tracer.Start(ctx, "AnalyticsService.ListExerciseProgress")
	defer span.End()

	return s.repo.ListExerciseProgress(ctx, userID)
}

// ListWorkoutSummaries возвращает список сводок тренировок.
func (s *AnalyticsService) ListWorkoutSummaries(ctx context.Context, userID string, limit, offset int) ([]domain.WorkoutSummary, error) {
	ctx, span := s.tracer.Start(ctx, "AnalyticsService.ListWorkoutSummaries")
	defer span.End()

	return s.repo.ListWorkoutSummaries(ctx, userID, limit, offset)
}
