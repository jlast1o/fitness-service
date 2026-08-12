package service

import (
	"context"
	"errors"
	"fitness-platform/pkg/logger"
	"fitness-platform/services/auth/internal/domain"
	"fitness-platform/services/auth/internal/repository"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrEmailPassRequired  = errors.New("email and password are required")
)

type AuthService struct {
	repo       repository.UserRepository
	jwtSecret  []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

func NewAuthService(repo repository.UserRepository, secret string, accessTTL, refreshTTL time.Duration) *AuthService {
	return &AuthService{repo: repo, jwtSecret: []byte(secret), accessTTL: accessTTL, refreshTTL: refreshTTL}
}

func (s *AuthService) Register(ctx context.Context, email, password string) (string, error) {
	if email == "" || password == "" {
		return "", ErrEmailPassRequired
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		logger.Log.Error().Err(err).Msg("failed to hash password")
		return "", err
	}

	user := &domain.User{
		Email:        email,
		PasswordHash: string(hash),
	}

	if err := s.repo.CreateUser(ctx, user); err != nil {
		logger.Log.Error().Err(err).Str("email", email).Msg("failed to create user")
		return "", err
	}

	return user.ID, nil
}

func (s *AuthService) Login(ctx context.Context, email, password string) (string, string, error) {
	if email == "" || password == "" {
		return "", "", ErrEmailPassRequired
	}

	user, err := s.repo.GetUserByEmail(ctx, email)

	if err != nil {
		logger.Log.Error().Err(err).Str("email", email).Msg("failed to get user")
		return "", "", err
	}
	if user == nil {
		return "", "", ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return "", "", ErrInvalidCredentials
		}
		logger.Log.Error().Err(err).Str("email", email).Msg("failed to compare password and hash")
		return "", "", err
	}

	accessToken, err := s.generateToken(user.ID, s.accessTTL)
	if err != nil {
		logger.Log.Error().Err(err).Msg("failed to generate access token")
		return "", "", err
	}

	refreshToken, err := s.generateToken(user.ID, s.refreshTTL)
	if err != nil {
		logger.Log.Error().Err(err).Msg("failed to generate refresh token")
		return "", "", err
	}

	return accessToken, refreshToken, nil
}

func (s *AuthService) generateToken(userID string, ttl time.Duration) (string, error) {
	claims := jwt.RegisteredClaims{
		Subject:   userID,
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	return token.SignedString(s.jwtSecret)
}
