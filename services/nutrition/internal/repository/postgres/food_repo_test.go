package postgres

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"fitness-platform/services/nutrition/internal/domain"
)

func testFoodRepository(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()

	const userA = "550e8400-e29b-41d4-a716-446655440021"
	const userB = "550e8400-e29b-41d4-a716-446655440022"

	repo := NewFoodRepo(pool, 2*time.Second)

	// Создаём личный продукт, игнорируя подменённого владельца.
	foodA, err := repo.CreatePersonalFood(ctx, userA, &domain.Food{
		OwnerUserID:     userB,
		Name:            "  Oatmeal A  ",
		Brand:           "Test Brand",
		CaloriesPer100g: 180,
		ProteinPer100g:  6,
		FatPer100g:      4,
		CarbsPer100g:    30,
	})
	if err != nil {
		t.Fatalf("create food A: %v", err)
	}

	if foodA.ID == "" || foodA.OwnerUserID != userA {
		t.Fatal("incorrect ID or owner for food A")
	}

	if foodA.Name != "Oatmeal A" {
		t.Fatalf("unexpected food name: %q", foodA.Name)
	}

	if foodA.CreatedAt.IsZero() || foodA.UpdatedAt.IsZero() {
		t.Fatal("food timestamps are empty")
	}

	foodB, err := repo.CreatePersonalFood(ctx, userB, &domain.Food{
		Name:            "Oatmeal B",
		CaloriesPer100g: 190,
		ProteinPer100g:  7,
		FatPer100g:      5,
		CarbsPer100g:    30,
	})
	if err != nil {
		t.Fatalf("create food B: %v", err)
	}

	// Общие продукты создаются системным кодом, не пользователем.
	var sharedID string

	err = pool.QueryRow(ctx, `
		INSERT INTO foods (
			name,
			calories_per_100g,
			protein_per_100g,
			fat_per_100g,
			carbs_per_100g
		)
		VALUES ('Shared Milk', 60, 3, 3.2, 4.8)
		RETURNING id::text
	`).Scan(&sharedID)
	if err != nil {
		t.Fatalf("create shared food: %v", err)
	}

	ownFood, err := repo.GetVisibleFood(ctx, userA, foodA.ID)
	if err != nil || ownFood == nil {
		t.Fatalf("read own food: %v", err)
	}

	sharedFood, err := repo.GetVisibleFood(ctx, userA, sharedID)
	if err != nil || sharedFood == nil {
		t.Fatalf("read shared food: %v", err)
	}

	if sharedFood.OwnerUserID != "" {
		t.Fatal("shared food must have no owner")
	}

	// Чужой личный продукт не должен быть доступен.
	foreignFood, err := repo.GetVisibleFood(
		ctx, userA, foodB.ID,
	)
	if err != nil {
		t.Fatalf("read foreign food: %v", err)
	}

	if foreignFood != nil {
		t.Fatal("user A can read user B's food")
	}

	foods, err := repo.ListVisibleFoods(ctx, userA, 100, 0)
	if err != nil {
		t.Fatalf("list foods: %v", err)
	}

	var foundOwn bool
	var foundShared bool

	for _, food := range foods {
		if food.ID == foodA.ID {
			foundOwn = true
		}
		if food.ID == sharedID {
			foundShared = true
		}
		if food.ID == foodB.ID {
			t.Fatal("user A can list user B's food")
		}
	}

	if !foundOwn || !foundShared {
		t.Fatal("own or shared food missing from catalog")
	}

	page, err := repo.ListVisibleFoods(ctx, userA, 1, 0)
	if err != nil {
		t.Fatalf("list paginated foods: %v", err)
	}

	if len(page) != 1 {
		t.Fatalf("expected 1 food on page, got %d", len(page))
	}

	_, err = repo.ListVisibleFoods(ctx, userA, 101, 0)
	if err == nil {
		t.Fatal("expected invalid pagination error")
	}

	updatedData := &domain.Food{
		Name:            "Updated Oatmeal",
		Brand:           "New Brand",
		CaloriesPer100g: 210,
		ProteinPer100g:  8.5,
		FatPer100g:      5,
		CarbsPer100g:    32,
	}

	// Пользователь B не может изменить продукт A.
	foreignUpdate, err := repo.UpdatePersonalFood(
		ctx, userB, foodA.ID, updatedData,
	)
	if err != nil {
		t.Fatalf("update foreign food: %v", err)
	}
	if foreignUpdate != nil {
		t.Fatal("user B updated user A's food")
	}

	// Общие продукты нельзя изменять через личный Repository.
	sharedUpdate, err := repo.UpdatePersonalFood(
		ctx, userA, sharedID, updatedData,
	)
	if err != nil {
		t.Fatalf("update shared food: %v", err)
	}
	if sharedUpdate != nil {
		t.Fatal("user updated shared food")
	}

	updated, err := repo.UpdatePersonalFood(
		ctx, userA, foodA.ID, updatedData,
	)
	if err != nil || updated == nil {
		t.Fatalf("update own food: %v", err)
	}

	if updated.Name != "Updated Oatmeal" {
		t.Fatalf("unexpected updated name: %q", updated.Name)
	}

	if math.Abs(updated.ProteinPer100g-8.5) > 0.000001 {
		t.Fatalf("unexpected protein: %v", updated.ProteinPer100g)
	}

	// Удалять чужие и общие продукты запрещено.
	deleted, err := repo.DeletePersonalFood(
		ctx, userB, foodA.ID,
	)
	if err != nil || deleted {
		t.Fatalf("user B deleted foreign food: %v", err)
	}

	deleted, err = repo.DeletePersonalFood(
		ctx, userA, sharedID,
	)
	if err != nil || deleted {
		t.Fatalf("user A deleted shared food: %v", err)
	}

	deleted, err = repo.DeletePersonalFood(
		ctx, userA, foodA.ID,
	)
	if err != nil || !deleted {
		t.Fatalf("delete own food: %v", err)
	}

	// После мягкого удаления продукт больше не доступен.
	missing, err := repo.GetVisibleFood(ctx, userA, foodA.ID)
	if err != nil {
		t.Fatalf("get deleted food: %v", err)
	}
	if missing != nil {
		t.Fatal("deleted food is still visible")
	}

	var deletedInDatabase bool
	err = pool.QueryRow(ctx, `
		SELECT deleted_at IS NOT NULL
		FROM foods
		WHERE id = $1::uuid
	`, foodA.ID).Scan(&deletedInDatabase)

	if err != nil {
		t.Fatalf("check soft deletion: %v", err)
	}
	if !deletedInDatabase {
		t.Fatal("food was not soft deleted")
	}

	deleted, err = repo.DeletePersonalFood(
		ctx, userA, foodA.ID,
	)
	if err != nil || deleted {
		t.Fatal("deleting the same food twice must return false")
	}
}
