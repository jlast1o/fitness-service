package postgres

import (
	"context"
	"errors"
	"fitness-platform/services/auth/internal/domain"
	"fitness-platform/services/auth/internal/repository"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type UserRepo struct {
	pool             *pgxpool.Pool
	operationTimeout time.Duration
}

func NewUserRepo(pool *pgxpool.Pool, operationTimeout time.Duration) repository.UserRepository {
	return &UserRepo{
		pool:             pool,
		operationTimeout: operationTimeout,
	}
}

func (r *UserRepo) CreateUser(ctx context.Context, user *domain.User) error {
	ctx, cancel := r.operationCtx(ctx)
	defer cancel()

	query := `INSERT INTO users (email, password_hash) VALUES ($1, $2) RETURNING id, created_at, updated_at`

	err := r.pool.QueryRow(ctx, query, user.Email, user.PasswordHash).Scan(&user.ID, &user.CreatedAt, &user.UpdatedAt)

	if err != nil {
		return fmt.Errorf("creating user: %w", err)
	}

	return nil
}

func (r *UserRepo) GetUserByEmail(ctx context.Context, email string) (*domain.User, error) {
	ctx, cancel := r.operationCtx(ctx)
	defer cancel()

	query := `SELECT id, email, password_hash, created_at, updated_at FROM users where email = $1`

	user := &domain.User{}
	err := r.pool.QueryRow(ctx, query, email).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.CreatedAt, &user.UpdatedAt)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get user by id: %w", err)
	}

	return user, nil
}

func (r *UserRepo) operationCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	if r.operationTimeout <= 0 {
		return context.WithCancel(ctx)
	}

	return context.WithTimeout(ctx, r.operationTimeout)
}
