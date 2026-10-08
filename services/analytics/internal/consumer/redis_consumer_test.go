package consumer

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"fitness-platform/pkg/testutil/redisstreamtest"
)

func TestRecoverPendingBatch_RespectsMinIdleAndACKs(
	t *testing.T,
) {
	client := redisstreamtest.NewClient(t)

	const (
		stream = "workout.events"
		group  = "analytics-recovery-test"
	)

	ctx := context.Background()

	// Неизвестный event type намеренно пропускается
	// обычной бизнес-логикой consumer'а и получает XACK.
	// Поэтому Analytics DB здесь не нужна.
	ids := redisstreamtest.MakePending(
		t,
		client,
		stream,
		group,
		`{
			"event_id":"11111111-1111-1111-1111-111111111111",
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
		consumer:     "analytics-recovery",
		claimMinIdle: time.Hour,
		claimCount:   10,
		claimStart:   "0-0",
	}

	// Сообщение ещё слишком молодое.
	// Восстанавливать его нельзя.
	c.recoverPendingBatch(ctx)

	require.EqualValues(
		t,
		1,
		redisstreamtest.PendingCount(t, client, stream, group),
	)

	pending, err := client.XPendingExt(
		ctx,
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

	require.Equal(t, "dead-consumer", pending[0].Consumer)
	require.Equal(t, ids[0], pending[0].ID)

	// Уменьшаем MinIdle, чтобы сообщение стало
	// подходящим для recovery.
	c.claimMinIdle = time.Millisecond

	time.Sleep(25 * time.Millisecond)

	c.recoverPendingBatch(ctx)

	// Recovery + обработка + XACK.
	// Сообщение должно исчезнуть из PEL.
	require.Zero(
		t,
		redisstreamtest.PendingCount(t, client, stream, group),
	)

	// Но XACK не удаляет сообщение из самого Stream.
	length, err := client.XLen(ctx, stream).Result()

	require.NoError(t, err)
	require.EqualValues(t, 1, length)
}
