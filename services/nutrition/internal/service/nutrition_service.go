package service

import (
	"context"
	"errors"

	"fitness-platform/pkg/logger"
	"fitness-platform/services/nutrition/internal/domain"
	"fitness-platform/services/nutrition/internal/repository"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

var (
	ErrInvalidInput = errors.New("invalid input")
)

type NutritionService struct {
	repo   repository.NutritionRepository
	tracer trace.Tracer
}

func NewNutritionService(
	repo repository.NutritionRepository,
) *NutritionService {
	return &NutritionService{
		repo:   repo,
		tracer: otel.Tracer("nutrition.service"),
	}
}

func (s *NutritionService) UpsertProfile(
	ctx context.Context,
	profile *domain.NutritionProfile,
) (*domain.NutritionProfile, error) {
	ctx, span := s.tracer.Start(
		ctx,
		"NutritionService.UpsertProfile",
	)
	defer span.End()

	if err := validateProfile(profile); err != nil {
		return nil, err
	}

	if err := s.repo.UpsertProfile(ctx, profile); err != nil {
		logger.FromContext(ctx).
			Error().
			Err(err).
			Str("user_id", profile.UserID).
			Msg("failed to upsert nutrition profile")

		return nil, err
	}

	savedProfile, err := s.repo.GetProfile(
		ctx,
		profile.UserID,
	)
	if err != nil {
		logger.FromContext(ctx).
			Error().
			Err(err).
			Str("user_id", profile.UserID).
			Msg("failed to get saved nutrition profile")

		return nil, err
	}

	return savedProfile, nil
}

func (s *NutritionService) GetProfile(
	ctx context.Context,
	userID string,
) (*domain.NutritionProfile, error) {
	ctx, span := s.tracer.Start(
		ctx,
		"NutritionService.GetProfile",
	)
	defer span.End()

	if userID == "" {
		return nil, ErrInvalidInput
	}

	profile, err := s.repo.GetProfile(ctx, userID)
	if err != nil {
		logger.FromContext(ctx).
			Error().
			Err(err).
			Str("user_id", userID).
			Msg("failed to get nutrition profile")

		return nil, err
	}

	return profile, nil
}

// GetTargets рассчитывает текущие дневные нормы питания пользователя.
func (s *NutritionService) GetTargets(
	ctx context.Context,
	userID string,
) (*NutritionTargets, error) {
	profile, err := s.GetProfile(ctx, userID)
	if err != nil {
		return nil, err
	}

	if profile == nil {
		return nil, nil
	}

	return CalculateTargets(profile)
}
