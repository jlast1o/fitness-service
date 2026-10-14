package eventdlq

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"fitness-platform/pkg/testutil/redisstreamtest"
)

func TestPublishAndAck_WritesOriginalBeforeAck(t *testing.T) {
	client := redisstreamtest.NewClient(t)
	ctx := context.Background()

	const stream = "test.events"
	const group = "test-group"

	ids := redisstreamtest.MakePending(
		t, client, stream, group, "{bad-json", 1,
	)

	message := redis.XMessage{
		ID: ids[0],
		Values: map[string]interface{}{
			"event": "{bad-json",
		},
	}

	err := PublishAndAck(
		ctx, client, stream, group,
		message, InvalidEnvelope, "invalid JSON",
	)

	require.NoError(t, err)

	// Оригинал больше не находится в PEL.
	require.Zero(t,
		redisstreamtest.PendingCount(
			t, client, stream, group,
		),
	)

	// А DLQ содержит оригинальное сообщение.
	entries, err := client.XRange(
		ctx,
		StreamName(stream, group),
		"-",
		"+",
	).Result()

	require.NoError(t, err)
	require.Len(t, entries, 1)

	require.Equal(
		t, ids[0],
		entries[0].Values["source_message_id"],
	)
	require.Equal(
		t, InvalidEnvelope,
		entries[0].Values["reason"],
	)

	var original map[string]interface{}

	require.NoError(t, json.Unmarshal(
		[]byte(
			entries[0].Values["original_values"].(string),
		),
		&original,
	))

	require.Equal(t, "{bad-json", original["event"])
}

func TestPublishAndAck_DLQFailureKeepsOriginalPending(t *testing.T) {
	client := redisstreamtest.NewClient(t)
	ctx := context.Background()

	const stream = "test.events"
	const group = "test-group"

	ids := redisstreamtest.MakePending(
		t, client, stream, group, "{bad-json", 1,
	)

	// Создаём Redis-пользователя, которому
	// запрещена команда XADD.
	err := client.Do(
		ctx,
		"ACL", "SETUSER", "no_dlq_xadd",
		"on",
		">password",
		"~*",
		"&*",
		"+@all",
		"-xadd",
	).Err()

	require.NoError(t, err)

	restricted := redis.NewClient(&redis.Options{
		Addr:     client.Options().Addr,
		Username: "no_dlq_xadd",
		Password: "password",
	})
	defer restricted.Close()

	require.NoError(t, restricted.Ping(ctx).Err())

	err = PublishAndAck(
		ctx,
		restricted,
		stream,
		group,
		redis.XMessage{
			ID: ids[0],
			Values: map[string]interface{}{
				"event": "{bad-json",
			},
		},
		InvalidEnvelope,
		"invalid JSON",
	)

	// DLQ недоступна — получили ошибку.
	require.Error(t, err)

	// Оригинал остался в PEL.
	require.EqualValues(
		t, 1,
		redisstreamtest.PendingCount(
			t, client, stream, group,
		),
	)

	// Никакой DLQ-записи не создано.
	exists, err := client.Exists(
		ctx,
		StreamName(stream, group),
	).Result()

	require.NoError(t, err)
	require.Zero(t, exists)
}
