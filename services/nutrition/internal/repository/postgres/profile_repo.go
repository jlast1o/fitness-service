package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fitness-platform/services/nutrition/internal/domain"
	"fitness-platform/services/nutrition/internal/repository"
)

type NutritionRepo struct {
	pool             *pgxpool.Pool
	operationTimeout time.Duration
}

func NewNutritionRepo(
	pool *pgxpool.Pool,
	operationTimeout time.Duration,
) repository.NutritionRepository {
	return &NutritionRepo{
		pool:             pool,
		operationTimeout: operationTimeout,
	}
}

func (r *NutritionRepo) UpsertProfile(
	ctx context.Context,
	profile *domain.NutritionProfile,
) error {
	ctx, cancel := r.operationCtx(ctx)
	defer cancel()

	query := `
		INSERT INTO nutrition_profiles (
			user_id,
			age,
			sex,
			height_cm,
			weight_kg,
			activity_level,
			goal
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (user_id)
		DO UPDATE SET
			age = EXCLUDED.age,
			sex = EXCLUDED.sex,
			height_cm = EXCLUDED.height_cm,
			weight_kg = EXCLUDED.weight_kg,
			activity_level = EXCLUDED.activity_level,
			goal = EXCLUDED.goal,
			updated_at = NOW()
	`

	_, err := r.pool.Exec(
		ctx,
		query,
		profile.UserID,
		profile.Age,
		profile.Sex,
		profile.HeightCm,
		profile.WeightKg,
		profile.ActivityLevel,
		profile.Goal,
	)
	if err != nil {
		return fmt.Errorf(
			"upsert nutrition profile: %w",
			err,
		)
	}

	return nil
}

func (r *NutritionRepo) GetProfile(
	ctx context.Context,
	userID string,
) (*domain.NutritionProfile, error) {
	ctx, cancel := r.operationCtx(ctx)
	defer cancel()

	query := `
		SELECT
			user_id,
			age,
			sex,
			height_cm,
			weight_kg,
			activity_level,
			goal,
			created_at,
			updated_at
		FROM nutrition_profiles
		WHERE user_id = $1
	`

	profile := &domain.NutritionProfile{}

	err := r.pool.QueryRow(
		ctx,
		query,
		userID,
	).Scan(
		&profile.UserID,
		&profile.Age,
		&profile.Sex,
		&profile.HeightCm,
		&profile.WeightKg,
		&profile.ActivityLevel,
		&profile.Goal,
		&profile.CreatedAt,
		&profile.UpdatedAt,
	)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}

		return nil, fmt.Errorf(
			"get nutrition profile: %w",
			err,
		)
	}

	return profile, nil
}

func (r *NutritionRepo) operationCtx(
	ctx context.Context,
) (context.Context, context.CancelFunc) {
	if r.operationTimeout <= 0 {
		return context.WithCancel(ctx)
	}

	return context.WithTimeout(
		ctx,
		r.operationTimeout,
	)
}
