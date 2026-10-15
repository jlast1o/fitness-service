package service

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"fitness-platform/services/nutrition/internal/domain"
)

const testFoodUserID = "550e8400-e29b-41d4-a716-446655440001"
const testFoodOtherUserID = "550e8400-e29b-41d4-a716-446655440002"
const testFoodID = "550e8400-e29b-41d4-a716-446655440003"

type fakeFoodRepository struct {
	food       *domain.Food
	foods      []domain.Food
	called     bool
	lastUserID string
	err        error
}

func (f *fakeFoodRepository) CreatePersonalFood(
	ctx context.Context,
	userID string,
	food *domain.Food,
) (*domain.Food, error) {
	f.called = true
	f.lastUserID = userID

	if f.err != nil {
		return nil, f.err
	}

	result := *food
	result.ID = testFoodID
	result.OwnerUserID = userID

	f.food = &result
	return &result, nil
}

func (f *fakeFoodRepository) GetVisibleFood(
	ctx context.Context,
	userID string,
	foodID string,
) (*domain.Food, error) {
	f.called = true
	f.lastUserID = userID

	if f.err != nil {
		return nil, f.err
	}

	if f.food == nil || f.food.ID != foodID {
		return nil, nil
	}

	return f.food, nil
}

func (f *fakeFoodRepository) ListVisibleFoods(
	ctx context.Context,
	userID string,
	limit int,
	offset int,
) ([]domain.Food, error) {
	f.called = true
	f.lastUserID = userID

	if f.err != nil {
		return nil, f.err
	}

	return f.foods, nil
}

func (f *fakeFoodRepository) UpdatePersonalFood(
	ctx context.Context,
	userID string,
	foodID string,
	food *domain.Food,
) (*domain.Food, error) {
	f.called = true
	f.lastUserID = userID

	if f.err != nil {
		return nil, f.err
	}

	if f.food == nil || f.food.ID != foodID {
		return nil, nil
	}

	result := *food
	result.ID = foodID
	result.OwnerUserID = userID
	f.food = &result

	return &result, nil
}

func (f *fakeFoodRepository) DeletePersonalFood(
	ctx context.Context,
	userID string,
	foodID string,
) (bool, error) {
	f.called = true
	f.lastUserID = userID

	if f.err != nil {
		return false, f.err
	}

	return f.food != nil && f.food.ID == foodID, nil
}

func validTestFood() *domain.Food {
	return &domain.Food{
		Name:            "Oatmeal",
		Brand:           "Test Brand",
		CaloriesPer100g: 180,
		ProteinPer100g:  6,
		FatPer100g:      4,
		CarbsPer100g:    30,
	}
}

func TestCreateFood(t *testing.T) {
	repo := &fakeFoodRepository{}
	svc := NewFoodService(repo)

	food := validTestFood()
	food.ID = testFoodID
	food.OwnerUserID = testFoodOtherUserID
	food.Name = "  Oatmeal  "
	food.Brand = "  Test Brand  "

	got, err := svc.CreateFood(
		context.Background(),
		testFoodUserID,
		food,
	)
	if err != nil {
		t.Fatalf("create food: %v", err)
	}

	if got.ID != testFoodID ||
		got.OwnerUserID != testFoodUserID {
		t.Fatal("incorrect food ID or owner")
	}

	if got.Name != "Oatmeal" || got.Brand != "Test Brand" {
		t.Fatal("food was not normalized")
	}

	if repo.lastUserID != testFoodUserID {
		t.Fatal("incorrect repository user ID")
	}

	// Нормализация не должна изменять исходный объект.
	if food.Name != "  Oatmeal  " {
		t.Fatal("input food was modified")
	}
}

func TestCreateFoodInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		change func(*domain.Food)
	}{
		{
			name: "empty name",
			change: func(f *domain.Food) {
				f.Name = "   "
			},
		},
		{
			name: "name too long",
			change: func(f *domain.Food) {
				f.Name = strings.Repeat("a", 201)
			},
		},
		{
			name: "brand too long",
			change: func(f *domain.Food) {
				f.Brand = strings.Repeat("b", 121)
			},
		},
		{
			name: "negative calories",
			change: func(f *domain.Food) {
				f.CaloriesPer100g = -1
			},
		},
		{
			name: "calories above maximum",
			change: func(f *domain.Food) {
				f.CaloriesPer100g = 1001
			},
		},
		{
			name: "negative protein",
			change: func(f *domain.Food) {
				f.ProteinPer100g = -1
			},
		},
		{
			name: "fat above maximum",
			change: func(f *domain.Food) {
				f.FatPer100g = 101
			},
		},
		{
			name: "negative carbs",
			change: func(f *domain.Food) {
				f.CarbsPer100g = -1
			},
		},
		{
			name: "NaN calories",
			change: func(f *domain.Food) {
				f.CaloriesPer100g = math.NaN()
			},
		},
		{
			name: "infinite protein",
			change: func(f *domain.Food) {
				f.ProteinPer100g = math.Inf(1)
			},
		},
		{
			name: "too many decimals",
			change: func(f *domain.Food) {
				f.FatPer100g = 3.123
			},
		},
		{
			name: "total macros exceed 100 grams",
			change: func(f *domain.Food) {
				f.ProteinPer100g = 80
				f.FatPer100g = 80
				f.CarbsPer100g = 80
			},
		},
		{
			name: "macros exceed rounding allowance",
			change: func(f *domain.Food) {
				f.ProteinPer100g = 40
				f.FatPer100g = 40
				f.CarbsPer100g = 20.2
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeFoodRepository{}
			svc := NewFoodService(repo)

			food := validTestFood()
			tt.change(food)

			_, err := svc.CreateFood(
				context.Background(),
				testFoodUserID,
				food,
			)

			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf(
					"expected ErrInvalidInput, got %v",
					err,
				)
			}

			if repo.called {
				t.Fatal("repository called with invalid food")
			}
		})
	}

	t.Run("nil food", func(t *testing.T) {
		repo := &fakeFoodRepository{}
		svc := NewFoodService(repo)

		_, err := svc.CreateFood(
			context.Background(),
			testFoodUserID,
			nil,
		)

		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("expected ErrInvalidInput, got %v", err)
		}

		if repo.called {
			t.Fatal("repository must not be called")
		}
	})

	t.Run("invalid user ID", func(t *testing.T) {
		repo := &fakeFoodRepository{}
		svc := NewFoodService(repo)

		_, err := svc.CreateFood(
			context.Background(),
			"not-a-uuid",
			validTestFood(),
		)

		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("expected ErrInvalidInput, got %v", err)
		}

		if repo.called {
			t.Fatal("repository must not be called")
		}
	})
}

func TestGetFood(t *testing.T) {
	food := validTestFood()
	food.ID = testFoodID

	repo := &fakeFoodRepository{food: food}
	svc := NewFoodService(repo)

	got, err := svc.GetFood(
		context.Background(),
		testFoodUserID,
		testFoodID,
	)
	if err != nil || got == nil {
		t.Fatalf("get food: %v", err)
	}

	if got.ID != testFoodID {
		t.Fatal("incorrect food ID")
	}

	if repo.lastUserID != testFoodUserID {
		t.Fatal("incorrect repository user ID")
	}

	missing, err := svc.GetFood(
		context.Background(),
		testFoodUserID,
		testFoodOtherUserID,
	)
	if err != nil || missing != nil {
		t.Fatal("expected missing food")
	}
}

func TestListFoods(t *testing.T) {
	repo := &fakeFoodRepository{
		foods: []domain.Food{*validTestFood()},
	}
	svc := NewFoodService(repo)

	foods, err := svc.ListFoods(
		context.Background(),
		testFoodUserID,
		20,
		0,
	)
	if err != nil {
		t.Fatalf("list foods: %v", err)
	}

	if len(foods) != 1 {
		t.Fatalf("expected 1 food, got %d", len(foods))
	}

	if repo.lastUserID != testFoodUserID {
		t.Fatal("incorrect repository user ID")
	}
}

func TestUpdateFood(t *testing.T) {
	existing := validTestFood()
	existing.ID = testFoodID

	repo := &fakeFoodRepository{food: existing}
	svc := NewFoodService(repo)

	update := validTestFood()
	update.Name = "  Updated Oatmeal  "

	got, err := svc.UpdateFood(
		context.Background(),
		testFoodUserID,
		testFoodID,
		update,
	)
	if err != nil || got == nil {
		t.Fatalf("update food: %v", err)
	}

	if got.Name != "Updated Oatmeal" {
		t.Fatal("food name was not normalized")
	}

	if got.OwnerUserID != testFoodUserID {
		t.Fatal("incorrect food owner")
	}
}

func TestDeleteFood(t *testing.T) {
	food := validTestFood()
	food.ID = testFoodID

	repo := &fakeFoodRepository{food: food}
	svc := NewFoodService(repo)

	deleted, err := svc.DeleteFood(
		context.Background(),
		testFoodUserID,
		testFoodID,
	)
	if err != nil || !deleted {
		t.Fatalf("delete food: %v", err)
	}

	if repo.lastUserID != testFoodUserID {
		t.Fatal("incorrect repository user ID")
	}
}

func TestFoodServiceInvalidIdentifiers(t *testing.T) {
	tests := []struct {
		name   string
		invoke func(*FoodService) error
	}{
		{
			name: "get invalid food ID",
			invoke: func(s *FoodService) error {
				_, err := s.GetFood(
					context.Background(),
					testFoodUserID,
					"invalid",
				)
				return err
			},
		},
		{
			name: "list invalid user ID",
			invoke: func(s *FoodService) error {
				_, err := s.ListFoods(
					context.Background(),
					"invalid",
					20,
					0,
				)
				return err
			},
		},
		{
			name: "list invalid limit",
			invoke: func(s *FoodService) error {
				_, err := s.ListFoods(
					context.Background(),
					testFoodUserID,
					101,
					0,
				)
				return err
			},
		},
		{
			name: "list negative offset",
			invoke: func(s *FoodService) error {
				_, err := s.ListFoods(
					context.Background(),
					testFoodUserID,
					20,
					-1,
				)
				return err
			},
		},
		{
			name: "update invalid food ID",
			invoke: func(s *FoodService) error {
				_, err := s.UpdateFood(
					context.Background(),
					testFoodUserID,
					"invalid",
					validTestFood(),
				)
				return err
			},
		},
		{
			name: "delete invalid food ID",
			invoke: func(s *FoodService) error {
				_, err := s.DeleteFood(
					context.Background(),
					testFoodUserID,
					"invalid",
				)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeFoodRepository{}
			svc := NewFoodService(repo)

			err := tt.invoke(svc)

			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("expected ErrInvalidInput, got %v", err)
			}

			if repo.called {
				t.Fatal("repository must not be called")
			}
		})
	}
}

func TestFoodServiceRepositoryError(t *testing.T) {
	dbErr := errors.New("database unavailable")

	repo := &fakeFoodRepository{err: dbErr}
	svc := NewFoodService(repo)

	_, err := svc.CreateFood(
		context.Background(),
		testFoodUserID,
		validTestFood(),
	)

	if !errors.Is(err, dbErr) {
		t.Fatalf("expected repository error, got %v", err)
	}
}

func TestFoodServiceUUIDNormalization(t *testing.T) {
	ctx := context.Background()

	repo := &fakeFoodRepository{}
	svc := NewFoodService(repo)

	userID := "urn:uuid:" + testFoodUserID
	foodID := "urn:uuid:" + testFoodID

	// При создании владелец должен быть нормализован.
	created, err := svc.CreateFood(
		ctx,
		userID,
		validTestFood(),
	)
	if err != nil {
		t.Fatalf("create food: %v", err)
	}

	if created.OwnerUserID != testFoodUserID {
		t.Fatal("user ID was not normalized")
	}

	// Поиск должен использовать стандартный UUID продукта.
	found, err := svc.GetFood(ctx, userID, foodID)
	if err != nil || found == nil {
		t.Fatalf("get food: %v", err)
	}

	if repo.lastUserID != testFoodUserID {
		t.Fatal("repository received non-canonical user ID")
	}

	// Список тоже должен принимать нормализованный ID.
	_, err = svc.ListFoods(ctx, userID, 20, 0)
	if err != nil {
		t.Fatalf("list foods: %v", err)
	}

	if repo.lastUserID != testFoodUserID {
		t.Fatal("list received non-canonical user ID")
	}

	// Обновление использует тот же формат идентификаторов.
	updated, err := svc.UpdateFood(
		ctx,
		userID,
		foodID,
		validTestFood(),
	)
	if err != nil || updated == nil {
		t.Fatalf("update food: %v", err)
	}

	// Удаление тоже не должно зависеть от формы записи UUID.
	deleted, err := svc.DeleteFood(ctx, userID, foodID)
	if err != nil || !deleted {
		t.Fatalf("delete food: %v", err)
	}
}

func TestCreateFoodAllowsLabelRounding(t *testing.T) {
	repo := &fakeFoodRepository{}
	svc := NewFoodService(repo)

	food := validTestFood()
	food.ProteinPer100g = 33.35
	food.FatPer100g = 33.35
	food.CarbsPer100g = 33.35

	// Сумма 100.05 г допустима с небольшим допуском.
	_, err := svc.CreateFood(
		context.Background(),
		testFoodUserID,
		food,
	)
	if err != nil {
		t.Fatalf("expected valid food, got %v", err)
	}

	if !repo.called {
		t.Fatal("repository was not called")
	}
}
