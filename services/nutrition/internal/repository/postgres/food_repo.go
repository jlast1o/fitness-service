package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fitness-platform/services/nutrition/internal/domain"
)

const foodColumns = `
	id::text,
	COALESCE(owner_user_id::text, ''),
	name,
	COALESCE(brand, ''),
	calories_per_100g,
	protein_per_100g,
	fat_per_100g,
	carbs_per_100g,
	created_at,
	updated_at
`

type FoodRepo struct {
	pool             *pgxpool.Pool
	operationTimeout time.Duration
}

func NewFoodRepo(
	pool *pgxpool.Pool,
	operationTimeout time.Duration,
) *FoodRepo {
	return &FoodRepo{
		pool:             pool,
		operationTimeout: operationTimeout,
	}
}

// CreatePersonalFood создаёт продукт для конкретного пользователя.
func (r *FoodRepo) CreatePersonalFood(
	ctx context.Context,
	userID string,
	food *domain.Food,
) (*domain.Food, error) {
	if food == nil {
		return nil, errors.New("food is nil")
	}

	ctx, cancel := r.operationCtx(ctx)
	defer cancel()

	query := `
		INSERT INTO foods (
			owner_user_id,
			name,
			brand,
			calories_per_100g,
			protein_per_100g,
			fat_per_100g,
			carbs_per_100g
		)
		VALUES (
			$1::uuid,
			BTRIM($2::text),
			NULLIF(BTRIM($3::text), ''),
			$4, $5, $6, $7
		)
		RETURNING ` + foodColumns

	result, err := scanFood(r.pool.QueryRow(
		ctx,
		query,
		userID,
		food.Name,
		food.Brand,
		food.CaloriesPer100g,
		food.ProteinPer100g,
		food.FatPer100g,
		food.CarbsPer100g,
	))
	if err != nil {
		return nil, fmt.Errorf("create personal food: %w", err)
	}

	return result, nil
}

// GetVisibleFood возвращает общий или принадлежащий пользователю продукт.
func (r *FoodRepo) GetVisibleFood(
	ctx context.Context,
	userID string,
	foodID string,
) (*domain.Food, error) {
	ctx, cancel := r.operationCtx(ctx)
	defer cancel()

	query := `
		SELECT ` + foodColumns + `
		FROM foods
		WHERE id = $2::uuid
		  AND deleted_at IS NULL
		  AND (
		      owner_user_id IS NULL
		      OR owner_user_id = $1::uuid
		  )
	`

	result, err := scanFood(r.pool.QueryRow(
		ctx,
		query,
		userID,
		foodID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get visible food: %w", err)
	}

	return result, nil
}

// ListVisibleFoods возвращает общие и личные продукты постранично.
func (r *FoodRepo) ListVisibleFoods(
	ctx context.Context,
	userID string,
	limit int,
	offset int,
) ([]domain.Food, error) {
	if limit < 1 || limit > 100 || offset < 0 {
		return nil, errors.New("invalid food pagination")
	}

	ctx, cancel := r.operationCtx(ctx)
	defer cancel()

	query := `
		SELECT ` + foodColumns + `
		FROM foods
		WHERE deleted_at IS NULL
		  AND (
		      owner_user_id IS NULL
		      OR owner_user_id = $1::uuid
		  )
		ORDER BY LOWER(name), id
		LIMIT $2 OFFSET $3
	`

	rows, err := r.pool.Query(
		ctx,
		query,
		userID,
		limit,
		offset,
	)
	if err != nil {
		return nil, fmt.Errorf("list visible foods: %w", err)
	}
	defer rows.Close()

	foods := make([]domain.Food, 0)

	for rows.Next() {
		food, err := scanFood(rows)
		if err != nil {
			return nil, fmt.Errorf("scan listed food: %w", err)
		}

		foods = append(foods, *food)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate visible foods: %w", err)
	}

	return foods, nil
}

// UpdatePersonalFood изменяет только активный личный продукт владельца.
func (r *FoodRepo) UpdatePersonalFood(
	ctx context.Context,
	userID string,
	foodID string,
	food *domain.Food,
) (*domain.Food, error) {
	if food == nil {
		return nil, errors.New("food is nil")
	}

	ctx, cancel := r.operationCtx(ctx)
	defer cancel()

	query := `
		UPDATE foods
		SET
			name = BTRIM($3::text),
			brand = NULLIF(BTRIM($4::text), ''),
			calories_per_100g = $5,
			protein_per_100g = $6,
			fat_per_100g = $7,
			carbs_per_100g = $8,
			updated_at = NOW()
		WHERE id = $2::uuid
		  AND owner_user_id = $1::uuid
		  AND deleted_at IS NULL
		RETURNING ` + foodColumns

	result, err := scanFood(r.pool.QueryRow(
		ctx,
		query,
		userID,
		foodID,
		food.Name,
		food.Brand,
		food.CaloriesPer100g,
		food.ProteinPer100g,
		food.FatPer100g,
		food.CarbsPer100g,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("update personal food: %w", err)
	}

	return result, nil
}

// DeletePersonalFood скрывает продукт, сохраняя его в базе.
func (r *FoodRepo) DeletePersonalFood(
	ctx context.Context,
	userID string,
	foodID string,
) (bool, error) {
	ctx, cancel := r.operationCtx(ctx)
	defer cancel()

	tag, err := r.pool.Exec(ctx, `
		UPDATE foods
		SET deleted_at = NOW(),
		    updated_at = NOW()
		WHERE id = $2::uuid
		  AND owner_user_id = $1::uuid
		  AND deleted_at IS NULL
	`, userID, foodID)

	if err != nil {
		return false, fmt.Errorf("delete personal food: %w", err)
	}

	return tag.RowsAffected() == 1, nil
}

type foodScanner interface {
	Scan(dest ...any) error
}

func scanFood(row foodScanner) (*domain.Food, error) {
	food := &domain.Food{}

	err := row.Scan(
		&food.ID,
		&food.OwnerUserID,
		&food.Name,
		&food.Brand,
		&food.CaloriesPer100g,
		&food.ProteinPer100g,
		&food.FatPer100g,
		&food.CarbsPer100g,
		&food.CreatedAt,
		&food.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	return food, nil
}

func (r *FoodRepo) operationCtx(
	ctx context.Context,
) (context.Context, context.CancelFunc) {
	if r.operationTimeout <= 0 {
		return context.WithCancel(ctx)
	}

	return context.WithTimeout(ctx, r.operationTimeout)
}
