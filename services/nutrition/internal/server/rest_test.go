package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"fitness-platform/services/nutrition/internal/domain"
	"fitness-platform/services/nutrition/internal/handler"
	postgresrepo "fitness-platform/services/nutrition/internal/repository/postgres"
	"fitness-platform/services/nutrition/internal/service"
)

const testJWTSecret = "nutrition-http-test-secret"

const userA = "550e8400-e29b-41d4-a716-446655440001"
const userB = "550e8400-e29b-41d4-a716-446655440002"

func TestNutritionProfileHTTP(t *testing.T) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Minute,
	)
	defer cancel()

	// 1. Запускаем PostgreSQL через Testcontainers.
	container, err := tcpostgres.Run(
		ctx,
		"postgres:15-alpine",
		tcpostgres.WithDatabase("nutrition_http_test"),
		tcpostgres.WithUsername("admin"),
		tcpostgres.WithPassword("secret"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start PostgreSQL: %v", err)
	}

	testcontainers.CleanupContainer(t, container)

	dbURL, err := container.ConnectionString(
		ctx,
		"sslmode=disable",
	)
	if err != nil {
		t.Fatalf("get database URL: %v", err)
	}

	// 2. Применяем настоящие миграции.
	source, err := iofs.New(
		os.DirFS("../../migrations"),
		".",
	)
	if err != nil {
		t.Fatalf("open migrations: %v", err)
	}

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

	if err := migrator.Up(); err != nil &&
		!errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("apply migrations: %v", err)
	}

	// 3. Подключаем настоящий PostgreSQL Repository.
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("create database pool: %v", err)
	}

	t.Cleanup(pool.Close)

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping PostgreSQL: %v", err)
	}

	repo := postgresrepo.NewNutritionRepo(
		pool,
		2*time.Second,
	)

	// 4. Создаём настоящий Service и Handler.
	nutritionService := service.NewNutritionService(repo)
	nutritionHandler := handler.NewNutritionHandler(
		nutritionService,
	)

	foodRepo := postgresrepo.NewFoodRepo(
		pool,
		2*time.Second,
	)

	foodService := service.NewFoodService(foodRepo)
	foodHandler := handler.NewFoodHandler(foodService)

	// 5. Запускаем временный HTTP-сервер.
	httpServer := httptest.NewServer(
		NewRouter(
			nutritionHandler,
			foodHandler,
			testJWTSecret,
			time.Second,
			func(ctx context.Context) error {
				return pool.Ping(ctx)
			},
		),
	)
	t.Cleanup(httpServer.Close)

	client := httpServer.Client()
	client.Timeout = 5 * time.Second

	// Создаём тестовые JWT для двух пользователей.
	tokenA := makeTestJWT(t, userA)
	tokenB := makeTestJWT(t, userB)

	initialBody := `{
		"age": 20,
		"sex": "female",
		"height_cm": 168,
		"weight_kg": 60,
		"activity_level": "moderate",
		"goal": "maintain"
	}`

	// Тест 1. Нельзя обращаться без JWT.
	t.Run("unauthorized", func(t *testing.T) {
		status, body := sendProfileRequest(
			t, client, httpServer.URL,
			http.MethodGet, "", "",
		)

		checkStatus(t, status, http.StatusUnauthorized, body)

		status, body = sendProfileRequest(
			t, client, httpServer.URL,
			http.MethodPost, "", initialBody,
		)

		checkStatus(t, status, http.StatusUnauthorized, body)
	})

	// Тест 2. Несуществующий профиль возвращает 404.
	t.Run("profile not found", func(t *testing.T) {
		status, body := sendProfileRequest(
			t, client, httpServer.URL,
			http.MethodGet, tokenA, "",
		)

		checkStatus(t, status, http.StatusNotFound, body)
	})

	var createdAt time.Time

	// Тест 3. Создание профиля через HTTP.
	t.Run("create profile", func(t *testing.T) {
		status, body := sendProfileRequest(
			t, client, httpServer.URL,
			http.MethodPost, tokenA, initialBody,
		)

		checkStatus(t, status, http.StatusOK, body)

		profile := decodeProfile(t, body)

		if profile.UserID != userA {
			t.Fatalf(
				"expected user %s, got %s",
				userA,
				profile.UserID,
			)
		}

		if profile.WeightKg != 60 {
			t.Fatalf(
				"expected weight 60, got %v",
				profile.WeightKg,
			)
		}

		if profile.CreatedAt.IsZero() {
			t.Fatal("created_at is empty")
		}

		createdAt = profile.CreatedAt
	})

	// Тест 4. Получение сохранённого профиля.
	t.Run("get profile", func(t *testing.T) {
		status, body := sendProfileRequest(
			t, client, httpServer.URL,
			http.MethodGet, tokenA, "",
		)

		checkStatus(t, status, http.StatusOK, body)

		profile := decodeProfile(t, body)

		if profile.UserID != userA {
			t.Fatal("incorrect user ID")
		}

		if profile.WeightKg != 60 {
			t.Fatal("incorrect profile weight")
		}
	})

	// Тест 5. Обновление веса через POST.
	t.Run("update profile", func(t *testing.T) {
		updatedBody := `{
			"age": 20,
			"sex": "female",
			"height_cm": 168,
			"weight_kg": 58.5,
			"activity_level": "moderate",
			"goal": "maintain"
		}`

		status, body := sendProfileRequest(
			t, client, httpServer.URL,
			http.MethodPost, tokenA, updatedBody,
		)

		checkStatus(t, status, http.StatusOK, body)

		profile := decodeProfile(t, body)

		if profile.WeightKg != 58.5 {
			t.Fatalf(
				"expected weight 58.5, got %v",
				profile.WeightKg,
			)
		}

		if !profile.CreatedAt.Equal(createdAt) {
			t.Fatal("created_at changed during update")
		}
	})

	// Тест 6. Некорректный возраст возвращает 400.
	t.Run("invalid age", func(t *testing.T) {
		badBody := `{
			"age": -5,
			"sex": "female",
			"height_cm": 168,
			"weight_kg": 60,
			"activity_level": "moderate",
			"goal": "maintain"
		}`

		status, body := sendProfileRequest(
			t, client, httpServer.URL,
			http.MethodPost, tokenA, badBody,
		)

		checkStatus(t, status, http.StatusBadRequest, body)

		// Тест 7. Некорректая цель питания не проходит
		t.Run("invalid goal", func(t *testing.T) {
			badBody := `{
			"age": 20,
			"sex": "female",
			"height_cm": 168,
			"weight_kg": 60,
			"activity_level": "moderate",
			"goal": "pizza"
		}`

			status, body := sendProfileRequest(
				t,
				client,
				httpServer.URL,
				http.MethodPost,
				tokenA,
				badBody,
			)

			checkStatus(
				t,
				status,
				http.StatusBadRequest,
				body,
			)
		})

		// Некорректный POST не должен менять вес.
		status, body = sendProfileRequest(
			t, client, httpServer.URL,
			http.MethodGet, tokenA, "",
		)

		checkStatus(t, status, http.StatusOK, body)

		profile := decodeProfile(t, body)

		if profile.WeightKg != 58.5 {
			t.Fatal("invalid request changed stored profile")
		}
	})

	// Тест 7. Пользователи не видят чужие профили.
	t.Run("user isolation", func(t *testing.T) {
		// Пользователь B ещё не создавал профиль.
		status, body := sendProfileRequest(
			t, client, httpServer.URL,
			http.MethodGet, tokenB, "",
		)

		checkStatus(t, status, http.StatusNotFound, body)

		// B пытается указать чужой user_id в JSON.
		spoofedBody := `{
			"user_id": "550e8400-e29b-41d4-a716-446655440001",
			"age": 25,
			"sex": "male",
			"height_cm": 180,
			"weight_kg": 80,
			"activity_level": "high",
			"goal": "maintain"
		}`

		status, body = sendProfileRequest(
			t, client, httpServer.URL,
			http.MethodPost, tokenB, spoofedBody,
		)

		checkStatus(t, status, http.StatusOK, body)

		profileB := decodeProfile(t, body)

		// Профиль должен принадлежать B, а не A.
		if profileB.UserID != userB {
			t.Fatalf(
				"expected user B, got %s",
				profileB.UserID,
			)
		}

		// Проверяем, что данные пользователя A не изменились.
		status, body = sendProfileRequest(
			t, client, httpServer.URL,
			http.MethodGet, tokenA, "",
		)

		checkStatus(t, status, http.StatusOK, body)

		profileA := decodeProfile(t, body)

		if profileA.WeightKg != 58.5 {
			t.Fatal("user A profile was changed by user B")
		}
	})

	t.Run("targets without JWT", func(t *testing.T) {
		status, body := sendTargetsRequest(
			t, client, httpServer.URL, "",
		)

		checkStatus(
			t,
			status,
			http.StatusUnauthorized,
			body,
		)
	})

	t.Run("targets without profile", func(t *testing.T) {
		tokenC := makeTestJWT(
			t,
			"550e8400-e29b-41d4-a716-446655440003",
		)

		status, body := sendTargetsRequest(
			t, client, httpServer.URL, tokenC,
		)

		checkStatus(
			t,
			status,
			http.StatusNotFound,
			body,
		)
	})

	t.Run("targets for user A", func(t *testing.T) {
		status, body := sendTargetsRequest(
			t, client, httpServer.URL, tokenA,
		)

		checkStatus(
			t,
			status,
			http.StatusOK,
			body,
		)

		var targets service.NutritionTargets
		if err := json.Unmarshal(body, &targets); err != nil {
			t.Fatalf("decode targets: %v", err)
		}

		// В предыдущем сценарии пользователь A
		// обновил вес с 60 до 58.5 кг.
		// Поэтому BMR и TDEE должны пересчитаться.
		if math.Abs(targets.BMR-1374) > 0.000001 {
			t.Fatalf("unexpected BMR: %v", targets.BMR)
		}

		if math.Abs(targets.TDEE-2129.7) > 0.000001 {
			t.Fatalf("unexpected TDEE: %v", targets.TDEE)
		}

		if math.Abs(targets.DailyCalories-2129.7) > 0.000001 {
			t.Fatalf(
				"unexpected daily calories: %v",
				targets.DailyCalories,
			)
		}

		// Проверяем, что количество калорий,
		// рассчитанное по БЖУ, совпадает с целью.
		totalCalories :=
			targets.ProteinGrams*4 +
				targets.FatGrams*9 +
				targets.CarbsGrams*4

		if math.Abs(totalCalories-targets.DailyCalories) > 0.000001 {
			t.Fatal("macros do not match daily calories")
		}
	})

	t.Run("targets for user B", func(t *testing.T) {
		status, body := sendTargetsRequest(
			t, client, httpServer.URL, tokenB,
		)

		checkStatus(
			t,
			status,
			http.StatusOK,
			body,
		)

		var targets service.NutritionTargets
		if err := json.Unmarshal(body, &targets); err != nil {
			t.Fatalf("decode targets: %v", err)
		}

		// Пользователь B имеет другой профиль:
		// male, 25 лет, 180 см, 80 кг, high.
		// Проверяем, что расчёт выполнен по его данным.
		if math.Abs(targets.BMR-1805) > 0.000001 {
			t.Fatalf("unexpected BMR: %v", targets.BMR)
		}

		if math.Abs(targets.TDEE-3113.625) > 0.000001 {
			t.Fatalf("unexpected TDEE: %v", targets.TDEE)
		}

		if math.Abs(targets.DailyCalories-3113.625) > 0.000001 {
			t.Fatalf(
				"unexpected daily calories: %v",
				targets.DailyCalories,
			)
		}
	})

	t.Run("create food without JWT", func(t *testing.T) {
		status, body := sendCreateFoodRequest(
			t, client, httpServer.URL, "",
			`{"name":"Oatmeal","calories_per_100g":180,"protein_per_100g":6,"fat_per_100g":4,"carbs_per_100g":30}`,
		)

		checkStatus(t, status, http.StatusUnauthorized, body)
	})

	t.Run("create food with invalid data", func(t *testing.T) {
		status, body := sendCreateFoodRequest(
			t, client, httpServer.URL, tokenA,
			`{"name":"Invalid","calories_per_100g":200,"protein_per_100g":80,"fat_per_100g":80,"carbs_per_100g":80}`,
		)

		checkStatus(t, status, http.StatusBadRequest, body)
	})

	t.Run("create personal food", func(t *testing.T) {
		status, body := sendCreateFoodRequest(
			t, client, httpServer.URL, tokenA,
			`{"name":"  Oatmeal  ","brand":"Test","calories_per_100g":180,"protein_per_100g":6,"fat_per_100g":4,"carbs_per_100g":30}`,
		)

		checkStatus(t, status, http.StatusCreated, body)

		var created domain.Food
		if err := json.Unmarshal(body, &created); err != nil {
			t.Fatalf("decode created food: %v", err)
		}

		if created.ID == "" || created.OwnerUserID != userA {
			t.Fatal("incorrect food ID or owner")
		}

		if created.Name != "Oatmeal" {
			t.Fatal("food name was not normalized")
		}

		saved, err := foodRepo.GetVisibleFood(ctx, userA, created.ID)
		if err != nil || saved == nil {
			t.Fatalf("food was not saved: %v", err)
		}

		foreign, err := foodRepo.GetVisibleFood(ctx, userB, created.ID)
		if err != nil {
			t.Fatalf("check foreign access: %v", err)
		}

		if foreign != nil {
			t.Fatal("user B can access user A's food")
		}
	})

	t.Run("list and get foods", func(t *testing.T) {
		own, err := foodRepo.CreatePersonalFood(
			ctx,
			userA,
			&domain.Food{
				Name:            "GET Test Own Food",
				CaloriesPer100g: 180,
				ProteinPer100g:  6,
				FatPer100g:      4,
				CarbsPer100g:    30,
			},
		)
		if err != nil {
			t.Fatalf("create own food: %v", err)
		}

		foreign, err := foodRepo.CreatePersonalFood(
			ctx,
			userB,
			&domain.Food{
				Name:            "GET Test Foreign Food",
				CaloriesPer100g: 100,
				ProteinPer100g:  5,
				FatPer100g:      2,
				CarbsPer100g:    15,
			},
		)
		if err != nil {
			t.Fatalf("create foreign food: %v", err)
		}

		var sharedID string
		err = pool.QueryRow(ctx, `
			INSERT INTO foods (
				name,
				calories_per_100g,
				protein_per_100g,
				fat_per_100g,
				carbs_per_100g
			)
			VALUES ('GET Test Shared Food', 60, 3, 3.2, 4.8)
			RETURNING id::text
		`).Scan(&sharedID)
		if err != nil {
			t.Fatalf("create shared food: %v", err)
		}

		status, body := sendFoodGetRequest(
			t, client, httpServer.URL,
			"/nutrition/foods", tokenA,
		)
		checkStatus(t, status, http.StatusOK, body)

		var foods []domain.Food
		if err := json.Unmarshal(body, &foods); err != nil {
			t.Fatalf("decode foods: %v", err)
		}

		foundOwn := false
		foundShared := false

		for _, food := range foods {
			if food.ID == own.ID {
				foundOwn = true
			}
			if food.ID == sharedID {
				foundShared = true
			}
			if food.ID == foreign.ID {
				t.Fatal("foreign food is visible in the list")
			}
		}

		if !foundOwn || !foundShared {
			t.Fatal("own or shared food missing from list")
		}

		status, body = sendFoodGetRequest(
			t, client, httpServer.URL,
			"/nutrition/foods/"+own.ID, tokenA,
		)
		checkStatus(t, status, http.StatusOK, body)

		var result domain.Food
		if err := json.Unmarshal(body, &result); err != nil {
			t.Fatalf("decode food: %v", err)
		}
		if result.ID != own.ID {
			t.Fatal("returned incorrect food")
		}

		status, body = sendFoodGetRequest(
			t, client, httpServer.URL,
			"/nutrition/foods/"+own.ID, tokenB,
		)
		checkStatus(t, status, http.StatusNotFound, body)

		status, body = sendFoodGetRequest(
			t, client, httpServer.URL,
			"/nutrition/foods/"+sharedID, tokenB,
		)
		checkStatus(t, status, http.StatusOK, body)

		status, body = sendFoodGetRequest(
			t, client, httpServer.URL,
			"/nutrition/foods/invalid-id", tokenA,
		)
		checkStatus(t, status, http.StatusBadRequest, body)

		status, body = sendFoodGetRequest(
			t, client, httpServer.URL,
			"/nutrition/foods?limit=101", tokenA,
		)
		checkStatus(t, status, http.StatusBadRequest, body)

		status, body = sendFoodGetRequest(
			t, client, httpServer.URL,
			"/nutrition/foods?limit=1", tokenA,
		)
		checkStatus(t, status, http.StatusOK, body)

		var page []domain.Food
		if err := json.Unmarshal(body, &page); err != nil {
			t.Fatalf("decode food page: %v", err)
		}
		if len(page) != 1 {
			t.Fatalf("expected 1 food, got %d", len(page))
		}

		status, body = sendFoodGetRequest(
			t, client, httpServer.URL,
			"/nutrition/foods", "",
		)
		checkStatus(t, status, http.StatusUnauthorized, body)
	})

	t.Run("update and delete foods", func(t *testing.T) {
		own, err := foodRepo.CreatePersonalFood(
			ctx,
			userA,
			&domain.Food{
				Name:            "Editable Food",
				CaloriesPer100g: 180,
				ProteinPer100g:  6,
				FatPer100g:      4,
				CarbsPer100g:    30,
			},
		)
		if err != nil {
			t.Fatalf("create test food: %v", err)
		}

		var sharedID string
		err = pool.QueryRow(ctx, `
			INSERT INTO foods (
				name,
				calories_per_100g,
				protein_per_100g,
				fat_per_100g,
				carbs_per_100g
			)
			VALUES ('Protected Shared Food', 60, 3, 3, 5)
			RETURNING id::text
		`).Scan(&sharedID)
		if err != nil {
			t.Fatalf("create shared food: %v", err)
		}

		path := "/nutrition/foods/" + own.ID
		sharedPath := "/nutrition/foods/" + sharedID

		validBody := `{
			"name":"  Updated Food  ",
			"brand":"Test",
			"calories_per_100g":210,
			"protein_per_100g":8,
			"fat_per_100g":5,
			"carbs_per_100g":32
		}`

		// Без авторизации изменять продукт нельзя.
		for _, method := range []string{http.MethodPut, http.MethodDelete} {
			status, body := sendFoodMutationRequest(
				t, client, httpServer.URL,
				method, path, "", validBody,
			)
			checkStatus(t, status, http.StatusUnauthorized, body)
		}

		// Пользователь B не может изменить или удалить продукт A.
		status, body := sendFoodMutationRequest(
			t, client, httpServer.URL,
			http.MethodPut, path, tokenB, validBody,
		)
		checkStatus(t, status, http.StatusNotFound, body)

		status, body = sendFoodMutationRequest(
			t, client, httpServer.URL,
			http.MethodDelete, path, tokenB, "",
		)
		checkStatus(t, status, http.StatusNotFound, body)

		// Общие продукты запрещено изменять и удалять.
		status, body = sendFoodMutationRequest(
			t, client, httpServer.URL,
			http.MethodPut, sharedPath, tokenA, validBody,
		)
		checkStatus(t, status, http.StatusNotFound, body)

		status, body = sendFoodMutationRequest(
			t, client, httpServer.URL,
			http.MethodDelete, sharedPath, tokenA, "",
		)
		checkStatus(t, status, http.StatusNotFound, body)

		// Некорректные данные не должны обновлять продукт.
		status, body = sendFoodMutationRequest(
			t, client, httpServer.URL,
			http.MethodPut, path, tokenA,
			`{"name":"Incomplete Food","calories_per_100g":100}`,
		)
		checkStatus(t, status, http.StatusBadRequest, body)

		status, body = sendFoodMutationRequest(
			t, client, httpServer.URL,
			http.MethodPut, "/nutrition/foods/invalid-id",
			tokenA, validBody,
		)
		checkStatus(t, status, http.StatusBadRequest, body)

		// Владелец успешно изменяет продукт.
		status, body = sendFoodMutationRequest(
			t, client, httpServer.URL,
			http.MethodPut, path, tokenA, validBody,
		)
		checkStatus(t, status, http.StatusOK, body)

		var updated domain.Food
		if err := json.Unmarshal(body, &updated); err != nil {
			t.Fatalf("decode updated food: %v", err)
		}

		if updated.ID != own.ID ||
			updated.OwnerUserID != userA ||
			updated.Name != "Updated Food" ||
			updated.CaloriesPer100g != 210 {
			t.Fatal("incorrect updated food")
		}

		saved, err := foodRepo.GetVisibleFood(ctx, userA, own.ID)
		if err != nil || saved == nil {
			t.Fatalf("get updated food: %v", err)
		}
		if saved.Name != "Updated Food" {
			t.Fatal("updated food was not saved")
		}

		// Некорректный ID при удалении возвращает 400.
		status, body = sendFoodMutationRequest(
			t, client, httpServer.URL,
			http.MethodDelete, "/nutrition/foods/invalid-id",
			tokenA, "",
		)
		checkStatus(t, status, http.StatusBadRequest, body)

		// Владелец успешно удаляет продукт.
		status, body = sendFoodMutationRequest(
			t, client, httpServer.URL,
			http.MethodDelete, path, tokenA, "",
		)
		checkStatus(t, status, http.StatusNoContent, body)

		if len(body) != 0 {
			t.Fatal("DELETE response must have an empty body")
		}

		// Удалённый продукт больше не доступен через GET.
		status, body = sendFoodGetRequest(
			t, client, httpServer.URL, path, tokenA,
		)
		checkStatus(t, status, http.StatusNotFound, body)

		// Повторное удаление возвращает 404.
		status, body = sendFoodMutationRequest(
			t, client, httpServer.URL,
			http.MethodDelete, path, tokenA, "",
		)
		checkStatus(t, status, http.StatusNotFound, body)

		// Запись сохраняется в базе с заполненным deleted_at.
		var softDeleted bool
		err = pool.QueryRow(ctx, `
			SELECT deleted_at IS NOT NULL
			FROM foods
			WHERE id = $1::uuid
		`, own.ID).Scan(&softDeleted)

		if err != nil {
			t.Fatalf("check soft delete: %v", err)
		}
		if !softDeleted {
			t.Fatal("food was not soft deleted")
		}
	})

}

// Создаёт JWT с указанным userID.
func makeTestJWT(t *testing.T, userID string) string {
	t.Helper()

	token := jwt.NewWithClaims(
		jwt.SigningMethodHS256,
		jwt.MapClaims{
			"sub": userID,
			"exp": time.Now().Add(time.Hour).Unix(),
		},
	)

	signed, err := token.SignedString(
		[]byte(testJWTSecret),
	)
	if err != nil {
		t.Fatalf("sign JWT: %v", err)
	}

	return signed
}

// Отправляет реальный HTTP-запрос тестовому серверу.
func sendProfileRequest(
	t *testing.T,
	client *http.Client,
	baseURL string,
	method string,
	token string,
	body string,
) (int, []byte) {
	t.Helper()

	req, err := http.NewRequest(
		method,
		baseURL+"/nutrition/profile",
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatalf("create HTTP request: %v", err)
	}

	if body != "" {
		req.Header.Set(
			"Content-Type",
			"application/json",
		)
	}

	if token != "" {
		req.Header.Set(
			"Authorization",
			"Bearer "+token,
		)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("send HTTP request: %v", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read HTTP response: %v", err)
	}

	return resp.StatusCode, responseBody
}

// Проверяет HTTP-статус.
func checkStatus(
	t *testing.T,
	got int,
	want int,
	body []byte,
) {
	t.Helper()

	if got != want {
		t.Fatalf(
			"expected HTTP %d, got %d; body: %s",
			want,
			got,
			body,
		)
	}
}

// Превращает JSON-ответ обратно в Go-структуру.
func decodeProfile(
	t *testing.T,
	body []byte,
) domain.NutritionProfile {
	t.Helper()

	var profile domain.NutritionProfile

	if err := json.Unmarshal(body, &profile); err != nil {
		t.Fatalf("decode profile: %v", err)
	}

	return profile
}

// sendTargetsRequest отправляет GET-запрос к API дневных норм.
// Используется только в HTTP-тестах.
func sendTargetsRequest(
	t *testing.T,
	client *http.Client,
	baseURL string,
	token string,
) (int, []byte) {
	t.Helper()

	req, err := http.NewRequest(
		http.MethodGet,
		baseURL+"/nutrition/targets",
		nil,
	)
	if err != nil {
		t.Fatalf("create HTTP request: %v", err)
	}

	if token != "" {
		req.Header.Set(
			"Authorization",
			"Bearer "+token,
		)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("send HTTP request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	return resp.StatusCode, body
}

func sendCreateFoodRequest(
	t *testing.T,
	client *http.Client,
	baseURL string,
	token string,
	body string,
) (int, []byte) {
	t.Helper()

	req, err := http.NewRequest(
		http.MethodPost,
		baseURL+"/nutrition/foods",
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatalf("create food request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("send food request: %v", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read food response: %v", err)
	}

	return resp.StatusCode, responseBody
}

// sendFoodGetRequest отправляет GET-запрос к каталогу продуктов.
func sendFoodGetRequest(
	t *testing.T,
	client *http.Client,
	baseURL string,
	path string,
	token string,
) (int, []byte) {
	t.Helper()

	req, err := http.NewRequest(
		http.MethodGet,
		baseURL+path,
		nil,
	)
	if err != nil {
		t.Fatalf("create food GET request: %v", err)
	}

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("send food GET request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read food response: %v", err)
	}

	return resp.StatusCode, body
}

// sendFoodMutationRequest отправляет PUT или DELETE в HTTP-тестах.
func sendFoodMutationRequest(
	t *testing.T,
	client *http.Client,
	baseURL string,
	method string,
	path string,
	token string,
	body string,
) (int, []byte) {
	t.Helper()

	req, err := http.NewRequest(
		method,
		baseURL+path,
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatalf("create food mutation request: %v", err)
	}

	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("send food mutation request: %v", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read food mutation response: %v", err)
	}

	return resp.StatusCode, responseBody
}
