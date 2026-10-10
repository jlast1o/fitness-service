package redisstartup

import (
	"context"
	"errors"
	"fitness-platform/pkg/backoff"
	"fitness-platform/pkg/logger"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type Runner struct {
	Client         redis.Cmdable
	Stream         string
	Group          string
	Service        string
	RecoverPending func(context.Context) error
	ReadNew        func(context.Context) error
	OnGroupReady   func()
}

func EnsureGroup(
	ctx context.Context,
	client redis.Cmdable,
	stream, group string,
) error {
	err := client.XGroupCreateMkStream(
		ctx, stream, group, "0",
	).Err()

	// Группа успешно создана.
	if err == nil {
		return nil
	}

	// Группа уже существует — это тоже успех.
	if redis.HasErrorPrefix(err, "BUSYGROUP") {
		return nil
	}

	// Только реальную ошибку возвращаем вызывающему коду.
	return fmt.Errorf(
		"ensure consumer group %q on stream %q: %w",
		group, stream, err,
	)
}

func IsNoGroup(err error) bool {
	for err != nil {
		if redis.HasErrorPrefix(err, "NOGROUP") {
			return true
		}
		err = errors.Unwrap(err)
	}

	return false
}

func (r Runner) Run(ctx context.Context) {
	retry, err := backoff.New(250*time.Millisecond, 30*time.Second)

	if err != nil {
		logger.Log.Error().Err(err).Msg("invalid Redis backoff config")
		return
	}

	groupReady := false

	for ctx.Err() == nil {
		if !groupReady {
			err := EnsureGroup(ctx, r.Client, r.Stream, r.Group)
			if err != nil {
				if !r.WaitRetry(ctx, retry, err, "consumer group initialization") {
					return
				}
				continue
			}

			groupReady = true

			if r.OnGroupReady != nil {
				r.OnGroupReady()
			}

			retry.Reset()

			logger.Log.Info().Str("service", r.Service).Str("group", r.Group).Msg("redis consumer group ready")
		}

		if err := r.RecoverPending(ctx); err != nil {
			if IsNoGroup(err) {
				groupReady = false
			}

			if !r.WaitRetry(ctx, retry, err, "XAUTOCLAIM") {
				return
			}
			continue
		}

		if err := r.ReadNew(ctx); err != nil {
			if IsNoGroup(err) {
				groupReady = false
			}

			if !r.WaitRetry(ctx, retry, err, "XREADGROUP") {
				return
			}

			continue
		}

		retry.Reset()

		if backoff.Wait(ctx, time.Second) != nil {
			return
		}
	}

}

func (r Runner) WaitRetry(ctx context.Context, retry *backoff.Exponential, cause error, operation string) bool {
	if ctx.Err() != nil {
		return false
	}

	delay := retry.Next()

	logger.Log.Warn().Err(cause).Str("service", r.Service).Str("operation", operation).Dur("delay", delay).Msg("Redis consumer operation failed")

	return backoff.Wait(ctx, delay) == nil
}
