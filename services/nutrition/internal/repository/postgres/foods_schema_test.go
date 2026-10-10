package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// testFoodsSchema проверяет структуру таблицы foods
// в настоящей PostgreSQL после применения миграций.
//
// Используем соединение из существующего интеграционного
// теста, чтобы не запускать ещё один контейнер.
func testFoodsSchema(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()

	t.Run("visibility and soft deletion", func(t *testing.T) {
		const userA = "550e8400-e29b-41d4-a716-446655440011"
		const userB = "550e8400-e29b-41d4-a716-446655440012"

		// Общий продукт не принадлежит конкретному пользователю.
		var foodID string
		var createdAt time.Time
		var updatedAt time.Time

		err := pool.QueryRow(ctx, `
			INSERT INTO foods (
				name,
				calories_per_100g,
				protein_per_100g,
				fat_per_100g,
				carbs_per_100g
			)
			VALUES ('Milk', 60, 3, 3.2, 4.8)
			RETURNING id::text, created_at, updated_at
		`).Scan(&foodID, &createdAt, &updatedAt)

		if err != nil {
			t.Fatalf("create shared food: %v", err)
		}

		if foodID == "" {
			t.Fatal("food ID was not generated")
		}

		if createdAt.IsZero() || updatedAt.IsZero() {
			t.Fatal("food timestamps were not generated")
		}

		// Создаём по одному личному продукту для двух пользователей.
		for _, tt := range []struct {
			userID string
			name   string
		}{
			{userA, "Oatmeal A"},
			{userB, "Oatmeal B"},
		} {
			_, err := pool.Exec(ctx, `
				INSERT INTO foods (
					owner_user_id,
					name,
					calories_per_100g,
					protein_per_100g,
					fat_per_100g,
					carbs_per_100g
				)
				VALUES ($1, $2, 180, 6, 4, 30)
			`, tt.userID, tt.name)

			if err != nil {
				t.Fatalf("create personal food: %v", err)
			}
		}

		// Бизнес-правило видимости для будущего Repository:
		// пользователь видит общие продукты и собственные,
		// но не чужие.
		for _, userID := range []string{userA, userB} {
			var count int

			err := pool.QueryRow(ctx, `
				SELECT COUNT(*)
				FROM foods
				WHERE deleted_at IS NULL
				  AND (
					owner_user_id IS NULL
					OR owner_user_id = $1
				  )
			`, userID).Scan(&count)

			if err != nil {
				t.Fatalf("count visible foods: %v", err)
			}

			if count != 2 {
				t.Fatalf(
					"expected 2 visible foods, got %d",
					count,
				)
			}
		}

		// Пользователь B не должен видеть личный продукт A.
		var foreignCount int

		err = pool.QueryRow(ctx, `
			SELECT COUNT(*)
			FROM foods
			WHERE name = 'Oatmeal A'
			  AND deleted_at IS NULL
			  AND (
				owner_user_id IS NULL
				OR owner_user_id = $1
			  )
		`, userB).Scan(&foreignCount)

		if err != nil {
			t.Fatalf("check food visibility: %v", err)
		}

		if foreignCount != 0 {
			t.Fatal("user B can see user A's personal food")
		}

		// Мягкое удаление скрывает продукт из обычного каталога.
		_, err = pool.Exec(ctx, `
			UPDATE foods
			SET deleted_at = NOW(),
			    updated_at = NOW()
			WHERE owner_user_id = $1
			  AND name = 'Oatmeal A'
		`, userA)

		if err != nil {
			t.Fatalf("soft delete food: %v", err)
		}

		var visibleAfterDelete int

		err = pool.QueryRow(ctx, `
			SELECT COUNT(*)
			FROM foods
			WHERE deleted_at IS NULL
			  AND (
				owner_user_id IS NULL
				OR owner_user_id = $1
			  )
		`, userA).Scan(&visibleAfterDelete)

		if err != nil {
			t.Fatalf("count foods after deletion: %v", err)
		}

		if visibleAfterDelete != 1 {
			t.Fatalf(
				"expected 1 visible food, got %d",
				visibleAfterDelete,
			)
		}
	})

	t.Run("database constraints", func(t *testing.T) {
		tests := []struct {
			name     string
			foodName string
			calories float64
			protein  float64
			fat      float64
			carbs    float64
		}{
			{
				name:     "empty name",
				foodName: "   ",
				calories: 100,
				protein:  5,
				fat:      1,
				carbs:    20,
			},
			{
				name:     "negative calories",
				foodName: "Invalid calories",
				calories: -10,
				protein:  5,
				fat:      1,
				carbs:    20,
			},
			{
				name:     "calories above maximum",
				foodName: "Too many calories",
				calories: 1001,
				protein:  5,
				fat:      1,
				carbs:    20,
			},
			{
				name:     "negative protein",
				foodName: "Invalid protein",
				calories: 100,
				protein:  -1,
				fat:      1,
				carbs:    20,
			},
			{
				name:     "protein above 100 grams",
				foodName: "Too much protein",
				calories: 100,
				protein:  101,
				fat:      1,
				carbs:    20,
			},
			{
				name:     "negative fat",
				foodName: "Invalid fat",
				calories: 100,
				protein:  5,
				fat:      -1,
				carbs:    20,
			},
			{
				name:     "carbs above 100 grams",
				foodName: "Too many carbs",
				calories: 100,
				protein:  5,
				fat:      1,
				carbs:    101,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, err := pool.Exec(ctx, `
					INSERT INTO foods (
						name,
						calories_per_100g,
						protein_per_100g,
						fat_per_100g,
						carbs_per_100g
					)
					VALUES ($1, $2, $3, $4, $5)
				`,
					tt.foodName,
					tt.calories,
					tt.protein,
					tt.fat,
					tt.carbs,
				)

				if err == nil {
					t.Fatal("expected database constraint error")
				}

				// PostgreSQL-код 23514 означает,
				// что нарушено ограничение CHECK.
				var pgErr *pgconn.PgError

				if !errors.As(err, &pgErr) ||
					pgErr.Code != "23514" {
					t.Fatalf(
						"expected CHECK violation, got %v",
						err,
					)
				}
			})
		}
	})
}
