package consumer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"fitness-platform/pkg/testutil/redisstreamtest"
)

type fakeSender struct {
	err   error
	calls int
}

func (s *fakeSender) Send(
	_ context.Context,
	_, _ string,
) error {
	s.calls++
	return s.err
}

const workoutCreatedEventJSON = `{
	"event_id":"44444444-4444-4444-4444-444444444444",
	"event_type":"workout.created",
	"event_version":1,
	"payload":{
		"user_id":"55555555-5555-5555-5555-555555555555",
		"name":"Test workout"
	}
}`

func TestRecoverPendingBatch_ACKsUnsupportedEvent(t *testing.T) {
	client := redisstreamtest.NewClient(t)

	const (
		stream = "workout.events"
		group  = "notification-recovery-ignored-test"
	)

	redisstreamtest.MakePending(
		t, client, stream, group,
		`{
			"event_id":"33333333-3333-3333-3333-333333333333",
			"event_type":"test.ignored",
			"event_version":1,
			"payload":{}
		}`,
		1,
	)

	c := &RedisConsumer{
		redisClient:  client,
		stream:       stream,
		group:        group,
		consumer:     "notification-recovery",
		claimMinIdle: time.Millisecond,
		claimStart:   "0-0",
	}

	time.Sleep(25 * time.Millisecond)
	c.recoverPendingBatch(context.Background())

	require.Zero(
		t,
		redisstreamtest.PendingCount(t, client, stream, group),
	)
}

func TestRecoverPendingBatch_SendFailureStaysPending(t *testing.T) {
	client := redisstreamtest.NewClient(t)

	const (
		stream = "workout.events"
		group  = "notification-recovery-failure-test"
	)

	ids := redisstreamtest.MakePending(
		t, client, stream, group,
		workoutCreatedEventJSON,
		1,
	)

	notificationSender := &fakeSender{
		err: errors.New("sender unavailable"),
	}

	c := &RedisConsumer{
		redisClient:  client,
		stream:       stream,
		group:        group,
		consumer:     "notification-recovery",
		sender:       notificationSender,
		claimMinIdle: time.Millisecond,
		claimStart:   "0-0",
	}

	time.Sleep(25 * time.Millisecond)
	c.recoverPendingBatch(context.Background())

	require.Equal(t, 1, notificationSender.calls)

	// Отправка не удалась: XACK не было.
	require.EqualValues(
		t,
		1,
		redisstreamtest.PendingCount(t, client, stream, group),
	)

	pending, err := client.XPendingExt(
		context.Background(),
		&redis.XPendingExtArgs{
			Stream: stream,
			Group:  group,
			Start:  "-",
			End:    "+",
			Count:  10,
		},
	).Result()

	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, ids[0], pending[0].ID)
	require.Equal(t, "notification-recovery", pending[0].Consumer)
}

func TestRecoverPendingBatch_RetryAfterSenderRecovery(t *testing.T) {
	client := redisstreamtest.NewClient(t)

	const (
		stream = "workout.events"
		group  = "notification-recovery-retry-test"
	)

	redisstreamtest.MakePending(
		t, client, stream, group,
		workoutCreatedEventJSON,
		1,
	)

	notificationSender := &fakeSender{
		err: errors.New("temporary failure"),
	}

	c := &RedisConsumer{
		redisClient:  client,
		stream:       stream,
		group:        group,
		consumer:     "notification-recovery",
		sender:       notificationSender,
		claimMinIdle: time.Millisecond,
		claimStart:   "0-0",
	}

	ctx := context.Background()

	// Первая попытка: отправка не удалась.
	time.Sleep(25 * time.Millisecond)
	c.recoverPendingBatch(ctx)

	require.EqualValues(
		t,
		1,
		redisstreamtest.PendingCount(t, client, stream, group),
	)

	// Восстанавливаем Sender.
	notificationSender.err = nil

	// Вторая попытка: успешная отправка и ACK.
	time.Sleep(25 * time.Millisecond)
	c.recoverPendingBatch(ctx)

	require.Equal(t, 2, notificationSender.calls)

	require.Zero(
		t,
		redisstreamtest.PendingCount(t, client, stream, group),
	)

	// Само сообщение остаётся в Redis Stream.
	length, err := client.XLen(ctx, stream).Result()

	require.NoError(t, err)
	require.EqualValues(t, 1, length)
}

func TestRecoverPendingBatch_AckFailureCausesRedelivery(
	t *testing.T,
) {
	ctx := context.Background()

	adminClient := redisstreamtest.NewClient(t)

	const (
		stream = "workout.events"
		group  = "notification-ack-failure-test"
	)

	// Создаём pending-сообщение.
	redisstreamtest.MakePending(
		t,
		adminClient,
		stream,
		group,
		workoutCreatedEventJSON,
		1,
	)

	// Создаём Redis-пользователя, которому разрешены
	// все команды, кроме XACK.
	err := adminClient.Do(
		ctx,
		"ACL", "SETUSER", "no_xack",
		"on",
		">test-password",
		"~*",
		"&*",
		"+@all",
		"-xack",
	).Err()

	require.NoError(t, err)

	restrictedClient := redis.NewClient(&redis.Options{
		Addr:     adminClient.Options().Addr,
		Username: "no_xack",
		Password: "test-password",
	})
	t.Cleanup(func() {
		_ = restrictedClient.Close()
	})

	require.NoError(t, restrictedClient.Ping(ctx).Err())

	notificationSender := &fakeSender{}

	firstConsumer := &RedisConsumer{
		redisClient:  restrictedClient,
		stream:       stream,
		group:        group,
		consumer:     "notification-first",
		sender:       notificationSender,
		claimMinIdle: time.Millisecond,
		claimStart:   "0-0",
	}

	time.Sleep(25 * time.Millisecond)

	// Send() успешен, но Redis отклонит XACK.
	firstConsumer.recoverPendingBatch(ctx)

	require.Equal(t, 1, notificationSender.calls)

	require.EqualValues(
		t,
		1,
		redisstreamtest.PendingCount(
			t, adminClient, stream, group,
		),
	)

	// Имитируем восстановление другим consumer'ом,
	// у которого есть разрешение выполнять XACK.
	secondConsumer := &RedisConsumer{
		redisClient:  adminClient,
		stream:       stream,
		group:        group,
		consumer:     "notification-second",
		sender:       notificationSender,
		claimMinIdle: time.Millisecond,
		claimStart:   "0-0",
	}

	time.Sleep(25 * time.Millisecond)

	secondConsumer.recoverPendingBatch(ctx)

	// Две успешные попытки Send().
	require.Equal(t, 2, notificationSender.calls)

	// Второй consumer смог выполнить XACK.
	require.Zero(
		t,
		redisstreamtest.PendingCount(
			t, adminClient, stream, group,
		),
	)
}
