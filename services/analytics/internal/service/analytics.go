package service

import (
	"context"
	"time"

	"fitness-platform/pkg/logger"
	"fitness-platform/services/analytics/internal/domain"
	"fitness-platform/services/analytics/internal/repository"
)

// AnalyticsService содержит бизнес-логику аналитики.
type AnalyticsService struct {
	repo repository.AnalyticsRepository
}

// NewAnalyticsService создаёт новый экземпляр AnalyticsService.
func NewAnalyticsService(repo repository.AnalyticsRepository) *AnalyticsService {
	return &AnalyticsService{repo: repo}
}

// ProcessWorkoutCreated обрабатывает событие о новой тренировке.
func (s *AnalyticsService) ProcessWorkoutCreated(ctx context.Context, event domain.WorkoutCreatedEvent) error {
	// 1. Считаем общий объём и агрегируем по упражнениям
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
		// Расчёт 1ПМ по формуле Эпли
		oneRM := calculate1RM(set.Weight, set.Reps)
		if oneRM > agg.Max1RM {
			agg.Max1RM = oneRM
		}
	}

	// 2. Обновляем user_stats
	stats, err := s.repo.GetUserStats(ctx, event.UserID)
	if err != nil {
		logger.Log.Error().Err(err).Msg("failed to get user stats")
		return err
	}
	if stats == nil {
		stats = &domain.UserStats{UserID: event.UserID}
	}
	stats.TotalWorkouts++
	stats.TotalVolume += totalVolume
	if stats.TotalVolume > 0 && totalRepsAll > 0 {
		stats.AvgIntensity = stats.TotalVolume / float64(totalRepsAll) // упрощённо: средний вес
	}
	// Здесь упрощённо; можно хранить сумму повторений для точности, но для MVP сойдёт.
	if err := s.repo.UpsertUserStats(ctx, stats); err != nil {
		logger.Log.Error().Err(err).Msg("failed to upsert user stats")
		return err
	}

	// 3. Обновляем exercise_progress для каждого упражнения
	for exerciseID, agg := range exerciseAgg {
		progress := &domain.ExerciseProgress{
			UserID:        event.UserID,
			ExerciseID:    exerciseID,
			BestWeight:    agg.BestWeight,
			TotalReps:     agg.TotalReps,
			LastWorkoutAt: agg.LastDate,
			Estimated1RM:  agg.Max1RM,
		}
		if err := s.repo.UpsertExerciseProgress(ctx, progress); err != nil {
			logger.Log.Error().Err(err).Msg("failed to upsert exercise progress")
			return err
		}
	}

	// 4. Вставляем сводку тренировки
	summary := &domain.WorkoutSummary{
		WorkoutID:   event.WorkoutID,
		UserID:      event.UserID,
		Name:        event.Name,
		Date:        event.Date,
		TotalVolume: totalVolume,
		SetCount:    event.SetsCount,
	}
	if err := s.repo.InsertWorkoutSummary(ctx, summary); err != nil {
		logger.Log.Error().Err(err).Msg("failed to insert workout summary")
		return err
	}

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
	return s.repo.GetUserStats(ctx, userID)
}

// GetExerciseProgress возвращает прогресс по конкретному упражнению.
func (s *AnalyticsService) GetExerciseProgress(ctx context.Context, userID, exerciseID string) (*domain.ExerciseProgress, error) {
	return s.repo.GetExerciseProgress(ctx, userID, exerciseID)
}

// ListExerciseProgress возвращает прогресс по всем упражнениям пользователя.
func (s *AnalyticsService) ListExerciseProgress(ctx context.Context, userID string) ([]domain.ExerciseProgress, error) {
	return s.repo.ListExerciseProgress(ctx, userID)
}

// ListWorkoutSummaries возвращает список сводок тренировок.
func (s *AnalyticsService) ListWorkoutSummaries(ctx context.Context, userID string, limit, offset int) ([]domain.WorkoutSummary, error) {
	return s.repo.ListWorkoutSummaries(ctx, userID, limit, offset)
}
