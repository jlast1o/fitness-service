package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"fitness-platform/services/nutrition/internal/domain"
)

func TestNutritionRepositoryIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Minute,
	)
	defer cancel()

	// 1. Запускаем временный PostgreSQL через Testcontainers.
	container, err := tcpostgres.Run(
		ctx,
		"postgres:15-alpine",
		tcpostgres.WithDatabase("nutrition_test"),
		tcpostgres.WithUsername("admin"),
		tcpostgres.WithPassword("secret"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start PostgreSQL container: %v", err)
	}

	// Контейнер удалится после завершения теста.
	testcontainers.CleanupContainer(t, container)

	// 2. Получаем адрес временной базы.
	dbURL, err := container.ConnectionString(
		ctx,
		"sslmode=disable",
	)
	if err != nil {
		t.Fatalf("get database URL: %v", err)
	}

	// 3. Загружаем настоящие миграции Nutrition.
	source, err := iofs.New(
		os.DirFS("../../../migrations"),
		".",
	)
	if err != nil {
		t.Fatalf("open migrations: %v", err)
	}

	// Используем PGX v5 вместо lib/pq.
	migrationURL := strings.Replace(
		dbURL,
		"postgres://",
		"pgx5://",
		1,
	)

	migrator, err := migrate.NewWithSourceInstance(
		"iofs",
		source,
		migrationURL,
	)
	if err != nil {
		t.Fatalf("create migrator: %v", err)
	}

	t.Cleanup(func() {
		sourceErr, databaseErr := migrator.Close()

		if sourceErr != nil {
			t.Errorf("close migration source: %v", sourceErr)
		}

		if databaseErr != nil {
			t.Errorf("close migration database: %v", databaseErr)
		}
	})

	// Применяем миграции.
	if err := migrator.Up(); err != nil &&
		!errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("apply migrations: %v", err)
	}

	// 4. Создаём пул соединений с PostgreSQL.
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("create database pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping PostgreSQL: %v", err)
	}

	// 5. Создаём настоящий Nutrition Repository.
	repo := NewNutritionRepo(
		pool,
		2*time.Second,
	)

	// Тест 1: создание и чтение профиля.
	t.Run("create and read profile", func(t *testing.T) {
		profile := testProfile(
			"550e8400-e29b-41d4-a716-446655440001",
		)

		err := repo.UpsertProfile(ctx, profile)
		if err != nil {
			t.Fatalf("save profile: %v", err)
		}

		saved, err := repo.GetProfile(
			ctx,
			profile.UserID,
		)
		if err != nil {
			t.Fatalf("get profile: %v", err)
		}

		if saved == nil {
			t.Fatal("profile not found")
		}

		if saved.WeightKg != profile.WeightKg {
			t.Fatalf(
				"expected weight %v, got %v",
				profile.WeightKg,
				saved.WeightKg,
			)
		}

		if saved.Sex != profile.Sex {
			t.Fatalf(
				"expected sex %s, got %s",
				profile.Sex,
				saved.Sex,
			)
		}

		if saved.CreatedAt.IsZero() ||
			saved.UpdatedAt.IsZero() {
			t.Fatal("timestamps were not set")
		}
	})

	// Тест 2: обновление существующего профиля.
	t.Run("upsert updates existing profile", func(t *testing.T) {
		profile := testProfile(
			"550e8400-e29b-41d4-a716-446655440002",
		)

		if err := repo.UpsertProfile(ctx, profile); err != nil {
			t.Fatalf("create profile: %v", err)
		}

		before, err := repo.GetProfile(
			ctx,
			profile.UserID,
		)
		if err != nil {
			t.Fatalf("get original profile: %v", err)
		}
		if before == nil {
			t.Fatal("original profile not found")
		}

		// Изменяем вес.
		profile.WeightKg = 58.5

		if err := repo.UpsertProfile(ctx, profile); err != nil {
			t.Fatalf("update profile: %v", err)
		}

		after, err := repo.GetProfile(
			ctx,
			profile.UserID,
		)
		if err != nil {
			t.Fatalf("get updated profile: %v", err)
		}
		if after == nil {
			t.Fatal("updated profile not found")
		}

		if after.WeightKg != 58.5 {
			t.Fatalf(
				"expected weight 58.5, got %v",
				after.WeightKg,
			)
		}

		// created_at должен остаться прежним.
		if !after.CreatedAt.Equal(before.CreatedAt) {
			t.Fatal("created_at changed during update")
		}

		// updated_at не должен стать раньше.
		if after.UpdatedAt.Before(before.UpdatedAt) {
			t.Fatal("updated_at moved backwards")
		}

		// Проверяем отсутствие дубликатов.
		var count int

		err = pool.QueryRow(
			ctx,
			`SELECT COUNT(*)
			 FROM nutrition_profiles
			 WHERE user_id = $1`,
			profile.UserID,
		).Scan(&count)

		if err != nil {
			t.Fatalf("count profiles: %v", err)
		}

		if count != 1 {
			t.Fatalf(
				"expected 1 profile, got %d",
				count,
			)
		}
	})

	// Тест 3: профиль не найден.
	t.Run("profile not found", func(t *testing.T) {
		profile, err := repo.GetProfile(
			ctx,
			"550e8400-e29b-41d4-a716-446655440003",
		)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if profile != nil {
			t.Fatal("expected nil profile")
		}
	})

	// Проверяем новую таблицу foods на той же тестовой PostgreSQL.
	// Так миграции и ограничения тестируются без второго контейнера.
	t.Run("foods schema", func(t *testing.T) {
		testFoodsSchema(t, ctx, pool)
	})

}

// Создаём тестовый профиль.
func testProfile(userID string) *domain.NutritionProfile {
	return &domain.NutritionProfile{
		UserID:        userID,
		Age:           20,
		Sex:           "female",
		HeightCm:      168,
		WeightKg:      60,
		ActivityLevel: "moderate",
		Goal:          "maintain",
	}
}
