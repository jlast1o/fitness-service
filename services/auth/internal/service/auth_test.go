package service_test

import (
	"context"
	"fitness-platform/services/auth/internal/domain"
	"fitness-platform/services/auth/internal/service"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

type MockUserRepo struct {
	mock.Mock
}

func (m *MockUserRepo) CreateUser(ctx context.Context, user *domain.User) error {
	args := m.Called(ctx, user)

	return args.Error(0)
}

func (m *MockUserRepo) GetUserByEmail(ctx context.Context, email string) (*domain.User, error) {
	args := m.Called(ctx, email)

	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*domain.User), args.Error(1)
}

func TestRegister_Success(t *testing.T) {
	// Arrange (подготовка)
	mockRepo := new(MockUserRepo)
	svc := service.NewAuthService(mockRepo, "test-secret", time.Hour, 24*time.Hour)

	// Ожидаем, что CreateUser будет вызван с любым контекстом и пользователем,
	// у которого email = "test@example.com" и password_hash не пустой.
	mockRepo.On("CreateUser", mock.Anything, mock.MatchedBy(func(u *domain.User) bool {
		return u.Email == "test@example.com" && u.PasswordHash != ""
	})).Return(nil).Run(func(args mock.Arguments) {
		// Симулируем, что БД заполнила ID и метки
		user := args.Get(1).(*domain.User)
		user.ID = "uuid-123"
		user.CreatedAt = time.Now()
		user.UpdatedAt = time.Now()
	})

	// Act (действие)
	userID, err := svc.Register(context.Background(), "test@example.com", "password123")

	// Assert (проверка)
	require.NoError(t, err)
	assert.Equal(t, "uuid-123", userID)
	mockRepo.AssertExpectations(t)
}

func TestRegister_EmptyEmailOrPassword(t *testing.T) {
	mockRepo := new(MockUserRepo)
	svc := service.NewAuthService(mockRepo, "test-secret", time.Hour, 24*time.Hour)

	_, err := svc.Register(context.Background(), "", "pass")
	require.Error(t, err)
	assert.ErrorIs(t, err, service.ErrEmailPassRequired)
	mockRepo.AssertNotCalled(t, "CreateUser")
}

func TestLogin_Success(t *testing.T) {
	mockRepo := new(MockUserRepo)
	svc := service.NewAuthService(mockRepo, "test-secret", time.Hour, 24*time.Hour)

	// Создаём реальный bcrypt хэш для пароля "correctpass"
	hash, err := bcrypt.GenerateFromPassword([]byte("correctpass"), bcrypt.DefaultCost)
	require.NoError(t, err)

	user := &domain.User{
		ID:           "uuid-123",
		Email:        "test@example.com",
		PasswordHash: string(hash),
	}

	// Ожидаем вызов GetUserByEmail
	mockRepo.On("GetUserByEmail", mock.Anything, "test@example.com").
		Return(user, nil)

	// Act
	accessToken, refreshToken, err := svc.Login(context.Background(), "test@example.com", "correctpass")

	// Assert
	require.NoError(t, err)
	assert.NotEmpty(t, accessToken)
	assert.NotEmpty(t, refreshToken)
	assert.NotEqual(t, accessToken, refreshToken)
	mockRepo.AssertExpectations(t)
}

func TestLogin_InvalidPassword(t *testing.T) {
	mockRepo := new(MockUserRepo)
	svc := service.NewAuthService(mockRepo, "test-secret", time.Hour, 24*time.Hour)

	// Хэш для "correctpass"
	hash, err := bcrypt.GenerateFromPassword([]byte("correctpass"), bcrypt.DefaultCost)
	require.NoError(t, err)

	user := &domain.User{
		ID:           "uuid-123",
		Email:        "test@example.com",
		PasswordHash: string(hash),
	}

	mockRepo.On("GetUserByEmail", mock.Anything, "test@example.com").
		Return(user, nil)

	// Act
	_, _, err = svc.Login(context.Background(), "test@example.com", "wrongpass")

	// Assert
	require.Error(t, err)
	assert.ErrorIs(t, err, service.ErrInvalidCredentials)
	mockRepo.AssertExpectations(t)
}

func TestLogin_UserNotFound(t *testing.T) {
	mockRepo := new(MockUserRepo)
	svc := service.NewAuthService(mockRepo, "test-secret", time.Hour, 24*time.Hour)

	// Пользователь не найден: возвращаем nil, nil
	mockRepo.On("GetUserByEmail", mock.Anything, "notfound@example.com").
		Return(nil, nil)

	_, _, err := svc.Login(context.Background(), "notfound@example.com", "any")
	require.Error(t, err)
	assert.ErrorIs(t, err, service.ErrInvalidCredentials)
	mockRepo.AssertExpectations(t)
}
