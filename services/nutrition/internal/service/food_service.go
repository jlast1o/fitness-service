package service

import (
	"context"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"fitness-platform/services/nutrition/internal/domain"
)

// FoodRepository описывает операции с продуктами, необходимые FoodService.
type FoodRepository interface {
	CreatePersonalFood(
		ctx context.Context,
		userID string,
		food *domain.Food,
	) (*domain.Food, error)

	GetVisibleFood(
		ctx context.Context,
		userID string,
		foodID string,
	) (*domain.Food, error)

	ListVisibleFoods(
		ctx context.Context,
		userID string,
		limit int,
		offset int,
	) ([]domain.Food, error)

	UpdatePersonalFood(
		ctx context.Context,
		userID string,
		foodID string,
		food *domain.Food,
	) (*domain.Food, error)

	DeletePersonalFood(
		ctx context.Context,
		userID string,
		foodID string,
	) (bool, error)
}

type FoodService struct {
	repo FoodRepository
}

func NewFoodService(repo FoodRepository) *FoodService {
	return &FoodService{repo: repo}
}

// CreateFood проверяет данные и создаёт личный продукт пользователя.
func (s *FoodService) CreateFood(
	ctx context.Context,
	userID string,
	food *domain.Food,
) (*domain.Food, error) {
	normalizedUserID, err := normalizeUUID(userID)
	if err != nil {
		return nil, err
	}

	normalized, err := normalizeFood(food)
	if err != nil {
		return nil, err
	}

	return s.repo.CreatePersonalFood(
		ctx,
		normalizedUserID,
		normalized,
	)
}

// GetFood возвращает общий или собственный продукт.
func (s *FoodService) GetFood(
	ctx context.Context,
	userID string,
	foodID string,
) (*domain.Food, error) {
	normalizedUserID, err := normalizeUUID(userID)
	if err != nil {
		return nil, err
	}

	normalizedFoodID, err := normalizeUUID(foodID)
	if err != nil {
		return nil, err
	}

	return s.repo.GetVisibleFood(
		ctx,
		normalizedUserID,
		normalizedFoodID,
	)
}

// ListFoods возвращает доступные пользователю продукты постранично.
func (s *FoodService) ListFoods(
	ctx context.Context,
	userID string,
	limit int,
	offset int,
) ([]domain.Food, error) {
	normalizedUserID, err := normalizeUUID(userID)
	if err != nil {
		return nil, err
	}

	if limit < 1 || limit > 100 || offset < 0 {
		return nil, ErrInvalidInput
	}

	return s.repo.ListVisibleFoods(
		ctx,
		normalizedUserID,
		limit,
		offset,
	)
}

// UpdateFood проверяет данные и изменяет личный продукт.
func (s *FoodService) UpdateFood(
	ctx context.Context,
	userID string,
	foodID string,
	food *domain.Food,
) (*domain.Food, error) {
	normalizedUserID, err := normalizeUUID(userID)
	if err != nil {
		return nil, err
	}

	normalizedFoodID, err := normalizeUUID(foodID)
	if err != nil {
		return nil, err
	}

	normalized, err := normalizeFood(food)
	if err != nil {
		return nil, err
	}

	return s.repo.UpdatePersonalFood(
		ctx,
		normalizedUserID,
		normalizedFoodID,
		normalized,
	)
}

// DeleteFood мягко удаляет личный продукт пользователя.
func (s *FoodService) DeleteFood(
	ctx context.Context,
	userID string,
	foodID string,
) (bool, error) {
	normalizedUserID, err := normalizeUUID(userID)
	if err != nil {
		return false, err
	}

	normalizedFoodID, err := normalizeUUID(foodID)
	if err != nil {
		return false, err
	}

	return s.repo.DeletePersonalFood(
		ctx,
		normalizedUserID,
		normalizedFoodID,
	)
}

// normalizeFood проверяет название, бренд и пищевую ценность продукта.
func normalizeFood(food *domain.Food) (*domain.Food, error) {
	if food == nil {
		return nil, ErrInvalidInput
	}

	normalized := *food
	normalized.Name = strings.TrimSpace(food.Name)
	normalized.Brand = strings.TrimSpace(food.Brand)

	if !utf8.ValidString(normalized.Name) ||
		!utf8.ValidString(normalized.Brand) ||
		utf8.RuneCountInString(normalized.Name) < 1 ||
		utf8.RuneCountInString(normalized.Name) > 200 ||
		utf8.RuneCountInString(normalized.Brand) > 120 {
		return nil, ErrInvalidInput
	}

	if !validNutritionValue(normalized.CaloriesPer100g, 1000) ||
		!validNutritionValue(normalized.ProteinPer100g, 100) ||
		!validNutritionValue(normalized.FatPer100g, 100) ||
		!validNutritionValue(normalized.CarbsPer100g, 100) {
		return nil, ErrInvalidInput
	}

	// Суммарная масса БЖУ не может существенно превышать 100 г.
	totalMacros := normalized.ProteinPer100g +
		normalized.FatPer100g +
		normalized.CarbsPer100g

	if totalMacros > 100.1+1e-9 {
		return nil, ErrInvalidInput
	}

	// Системные идентификаторы нельзя задавать через данные продукта.
	normalized.ID = ""
	normalized.OwnerUserID = ""

	return &normalized, nil
}

// validNutritionValue проверяет границы и точность до двух знаков.
func validNutritionValue(value float64, max float64) bool {
	if math.IsNaN(value) || math.IsInf(value, 0) ||
		value < 0 || value > max {
		return false
	}

	scaled := value * 100

	return math.Abs(scaled-math.Round(scaled)) < 1e-7
}

// normalizeUUID приводит идентификатор к стандартному UUID-формату.
func normalizeUUID(value string) (string, error) {
	id, err := uuid.Parse(value)
	if err != nil {
		return "", ErrInvalidInput
	}

	return id.String(), nil
}
