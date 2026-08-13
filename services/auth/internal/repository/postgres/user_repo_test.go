package postgres_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"fitness-platform/services/auth/internal/domain"
	"fitness-platform/services/auth/internal/repository/postgres"
)

// setupTestDB поднимает контейнер PostgreSQL, применяет миграции и возвращает пул соединений + функцию очистки.
func setupTestDB(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()
	t.Setenv("TESTCONTAINERS_HOST_OVERRIDE", "127.0.0.1")
	ctx := context.Background()

	// 1. Описываем контейнер PostgreSQL
	req := testcontainers.ContainerRequest{
		Image:        "postgres:15-alpine",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "test",
			"POSTGRES_PASSWORD": "test",
			"POSTGRES_DB":       "testdb",
		},
		WaitingFor: wait.ForListeningPort("5432/tcp"),
	}

	// 2. Запускаем контейнер
	postgresContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)

	// 3. Получаем хост и порт, на котором контейнер слушает PostgreSQL
	host, err := postgresContainer.Host(ctx)
	require.NoError(t, err)
	port, err := postgresContainer.MappedPort(ctx, "5432")
	require.NoError(t, err)

	dbURL := fmt.Sprintf("postgres://test:test@%s:%s/testdb?sslmode=disable", host, port.Port())

	// 4. Даём базе немного времени на инициализацию (не обязательно, но надёжно)
	time.Sleep(2 * time.Second)

	// 5. Применяем миграции
	m, err := migrate.New("file://../../../migrations", dbURL)
	require.NoError(t, err)
	err = m.Up()
	require.NoError(t, err)

	// 6. Создаём пул соединений
	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)

	// 7. Функция очистки: закрываем пул и останавливаем контейнер
	cleanup := func() {
		pool.Close()
		if err := postgresContainer.Terminate(ctx); err != nil {
			t.Logf("failed to terminate container: %v", err)
		}
	}

	return pool, cleanup
}

func TestCreateUser(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	repo := postgres.NewUserRepo(pool)

	user := &domain.User{
		Email:        "john@doe.com",
		PasswordHash: "hashed_password",
	}

	err := repo.CreateUser(context.Background(), user)
	require.NoError(t, err)
	require.NotEmpty(t, user.ID)
	require.False(t, user.CreatedAt.IsZero())
	require.False(t, user.UpdatedAt.IsZero())
}

func TestGetUserByEmail(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	repo := postgres.NewUserRepo(pool)

	// Сначала создаём пользователя
	user := &domain.User{
		Email:        "jane@doe.com",
		PasswordHash: "hashed_password",
	}
	err := repo.CreateUser(context.Background(), user)
	require.NoError(t, err)

	// Ищем по email
	found, err := repo.GetUserByEmail(context.Background(), "jane@doe.com")
	require.NoError(t, err)
	require.NotNil(t, found)
	require.Equal(t, user.ID, found.ID)
	require.Equal(t, "jane@doe.com", found.Email)
	require.Equal(t, "hashed_password", found.PasswordHash)
}

func TestGetUserByEmail_NotFound(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	repo := postgres.NewUserRepo(pool)

	found, err := repo.GetUserByEmail(context.Background(), "nonexistent@example.com")
	require.NoError(t, err)
	require.Nil(t, found)
}
