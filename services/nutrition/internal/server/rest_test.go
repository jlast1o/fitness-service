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

	// 5. Запускаем временный HTTP-сервер.
	httpServer := httptest.NewServer(
		NewRouter(
			nutritionHandler,
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
