package redisstreamtest

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// commandRecorder записывает время вызовов Redis-команд.
// Это позволяет измерять реальные интервалы retry.
type commandRecorder struct {
	mu    sync.Mutex
	calls map[string][]time.Time
}

func (r *commandRecorder) DialHook(
	next redis.DialHook,
) redis.DialHook {
	return next
}

func (r *commandRecorder) ProcessHook(
	next redis.ProcessHook,
) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		name := cmd.Name()

		if name == "xautoclaim" || name == "xreadgroup" {
			r.mu.Lock()
			r.calls[name] = append(
				r.calls[name],
				time.Now(),
			)
			r.mu.Unlock()
		}

		return next(ctx, cmd)
	}
}

func (r *commandRecorder) ProcessPipelineHook(
	next redis.ProcessPipelineHook,
) redis.ProcessPipelineHook {
	return next
}

func (r *commandRecorder) times(
	name string,
) []time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]time.Time(nil), r.calls[name]...)
}

// AssertBackoffAndRecovery проверяет настоящий consumer Run()
// на Redis 7 через Testcontainers.
//
// Временно запрещаем Redis-команды через ACL,
// наблюдаем retry, восстанавливаем разрешения
// и проверяем успешную обработку сообщения.
func AssertBackoffAndRecovery(
	t *testing.T,
	run func(
		ctx context.Context,
		client *redis.Client,
		stream, group string,
	),
) {
	t.Helper()

	ctx := context.Background()
	admin := NewClient(t)

	const (
		stream   = "workout.events"
		group    = "backoff-integration-group"
		username = "backoff_test"
		password = "test-password"
	)

	// Создаём consumer group.
	require.NoError(
		t,
		admin.XGroupCreateMkStream(
			ctx, stream, group, "0",
		).Err(),
	)

	// Неподдерживаемый тип события:
	// consumer обработает его без обращения
	// к бизнес-сервису и выполнит XACK.
	eventID, err := admin.XAdd(
		ctx,
		&redis.XAddArgs{
			Stream: stream,
			Values: map[string]any{
				"event": `{
					"event_id":"00000000-0000-4000-8000-000000000001",
					"event_type":"test.ignored",
					"event_version":1,
					"payload":{}
				}`,
			},
		},
	).Result()
	require.NoError(t, err)

	// Тестовый пользователь имеет доступ ко всем
	// командам, кроме XAUTOCLAIM.
	require.NoError(
		t,
		admin.Do(
			ctx,
			"ACL", "SETUSER", username,
			"on",
			">"+password,
			"~*",
			"&*",
			"+@all",
			"-xautoclaim",
		).Err(),
	)

	client := redis.NewClient(&redis.Options{
		Addr:         admin.Options().Addr,
		Username:     username,
		Password:     password,
		MaxRetries:   -1,
		DialTimeout:  time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: time.Second,
	})

	t.Cleanup(func() {
		_ = client.Close()
	})

	require.NoError(t, client.Ping(ctx).Err())

	recorder := &commandRecorder{
		calls: make(map[string][]time.Time),
	}

	client.AddHook(recorder)

	// Запускаем настоящий consumer.Run().
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	go func() {
		defer close(done)
		run(runCtx, client, stream, group)
	}()

	t.Cleanup(func() {
		cancel()

		select {
		case <-done:
		case <-time.After(4 * time.Second):
			t.Error(
				"consumer did not stop after cancellation",
			)
		}
	})

	// ЭТАП 1. Redis возвращает NOPERM.
	// Ожидаем минимум три попытки XAUTOCLAIM.
	require.Eventually(
		t,
		func() bool {
			return len(
				recorder.times("xautoclaim"),
			) >= 3
		},
		8*time.Second,
		20*time.Millisecond,
		"XAUTOCLAIM must be retried",
	)

	claims := recorder.times("xautoclaim")

	firstGap := claims[1].Sub(claims[0])
	secondGap := claims[2].Sub(claims[1])

	// Проверяем диапазоны с запасом на планировщик ОС.
	// Точные диапазоны jitter проверяют unit-тесты.
	require.GreaterOrEqual(
		t, firstGap, 100*time.Millisecond,
	)
	require.Less(
		t, firstGap, 800*time.Millisecond,
	)

	require.GreaterOrEqual(
		t, secondGap, 200*time.Millisecond,
	)
	require.Less(
		t, secondGap, 1200*time.Millisecond,
	)

	// ЭТАП 2. Восстанавливаем XAUTOCLAIM.
	require.NoError(
		t,
		admin.Do(
			ctx,
			"ACL", "SETUSER",
			username, "+xautoclaim",
		).Err(),
	)

	// Consumer должен сам восстановить работу,
	// прочитать событие и подтвердить его.
	require.Eventually(
		t,
		func() bool {
			groups, err := admin.XInfoGroups(
				ctx, stream,
			).Result()

			return err == nil &&
				len(groups) == 1 &&
				groups[0].LastDeliveredID == eventID &&
				groups[0].Pending == 0
		},
		8*time.Second,
		25*time.Millisecond,
		"consumer must recover and ACK the event",
	)

	// ЭТАП 3. Проверяем Reset после успеха.
	// Теперь запрещаем XREADGROUP.
	readsBefore := len(
		recorder.times("xreadgroup"),
	)

	require.NoError(
		t,
		admin.Do(
			ctx,
			"ACL", "SETUSER",
			username, "-xreadgroup",
		).Err(),
	)

	require.Eventually(
		t,
		func() bool {
			return len(
				recorder.times("xreadgroup"),
			) >= readsBefore+3
		},
		8*time.Second,
		20*time.Millisecond,
		"XREADGROUP must be retried",
	)

	reads := recorder.times("xreadgroup")

	resetGap := reads[readsBefore+1].Sub(
		reads[readsBefore],
	)

	// После предыдущего успешного прохода
	// backoff должен начаться с base delay,
	// а не продолжить старую последовательность.
	require.GreaterOrEqual(
		t, resetGap, 100*time.Millisecond,
	)
	require.Less(
		t,
		resetGap,
		850*time.Millisecond,
		"backoff should restart after recovery",
	)

	// ЭТАП 4. Отменяем контекст во время ошибок.
	// Consumer должен завершиться без ожидания
	// полного backoff delay.
	started := time.Now()
	cancel()

	select {
	case <-done:
		require.Less(
			t,
			time.Since(started),
			time.Second,
		)

	case <-time.After(time.Second):
		t.Fatal(
			"consumer did not stop promptly during backoff",
		)
	}
}
