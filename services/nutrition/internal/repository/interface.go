package repository

import (
	"context"
	"fitness-platform/services/nutrition/internal/domain"
)

// NutritionRepository описывает, что Nutrition Service умеет делать с данными профиля.
type NutritionRepository interface {
	//создание и обновление профиля
	UpsertProfile(
		ctx context.Context,
		profile *domain.NutritionProfile,
	) error

	//получение профиля
	GetProfile(
		ctx context.Context,
		userID string,
	) (*domain.NutritionProfile, error)
}
