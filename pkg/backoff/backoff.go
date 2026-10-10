package backoff

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"
)

type Exponential struct {
	base    time.Duration
	max     time.Duration
	current time.Duration
}

// new backoff struct
func New(base, max time.Duration) (*Exponential, error) {
	if base <= 0 {
		return nil, errors.New("backoff delay must be positive")
	}

	if max < base {
		return nil, errors.New("backoff maximum delay must be >= base delay")
	}

	return &Exponential{
		base: base,
		max:  max,
	}, nil
}

func (b *Exponential) Next() time.Duration {
	switch {
	case b.current == 0:
		b.current = b.base
	case b.current >= b.max/2:
		b.current = b.max
	default:
		b.current *= 2
	}

	// Задержка случайно выбирается в диапазоне
	// от 50% до 100% текущего лимита
	half := b.current / 2

	return half + time.Duration(
		rand.Int64N(int64(b.current-half)+1),
	)
}

// Reset сбрасывает backoff после успешного
// обращения к Redis.
func (b *Exponential) Reset() {
	b.current = 0
}

// Wait ожидает задержку с поддержкой
// отмены через context.
func Wait(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()

	case <-timer.C:
		return ctx.Err()
	}
}
