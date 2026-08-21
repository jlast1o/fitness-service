package service

import (
	"context"
	"errors"

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
