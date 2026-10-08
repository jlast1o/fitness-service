package redisstreamtest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// NewClient поднимает настоящий Redis через Testcontainers.
func NewClient(t *testing.T) *redis.Client {
	t.Helper()

	ctx := context.Background()

	container, err := testcontainers.GenericContainer(
		ctx,
		testcontainers.GenericContainerRequest{
			ContainerRequest: testcontainers.ContainerRequest{
				Image:        "redis:7-alpine",
				ExposedPorts: []string{"6379/tcp"},
				WaitingFor: wait.
					ForLog("Ready to accept connections").
					WithStartupTimeout(45 * time.Second),
			},
			Started: true,
		},
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Logf("terminate Redis container: %v", err)
		}
	})

	host, err := container.Host(ctx)
	require.NoError(t, err)

	port, err := container.MappedPort(ctx, "6379/tcp")
	require.NoError(t, err)

	client := redis.NewClient(&redis.Options{
		Addr: fmt.Sprintf("%s:%s", host, port.Port()),
	})

	t.Cleanup(func() {
		_ = client.Close()
	})

	require.NoError(t, client.Ping(ctx).Err())

	return client
}

// MakePending создаёт сообщения и отдаёт их consumer'у,
// который намеренно не подтверждает их через XACK.
func MakePending(
	t *testing.T,
	client *redis.Client,
	stream string,
	group string,
	eventJSON string,
	count int,
) []string {
	t.Helper()

	ctx := context.Background()

	err := client.XGroupCreateMkStream(
		ctx,
		stream,
		group,
		"0",
	).Err()
	require.NoError(t, err)

	ids := make([]string, 0, count)

	for i := 0; i < count; i++ {
		id, err := client.XAdd(
			ctx,
			&redis.XAddArgs{
				Stream: stream,
				Values: map[string]interface{}{
					"event": eventJSON,
				},
			},
		).Result()
		require.NoError(t, err)

		ids = append(ids, id)
	}

	// Симулируем первого consumer'а.
	// Он получает сообщения, но не делает XACK.
	streams, err := client.XReadGroup(
		ctx,
		&redis.XReadGroupArgs{
			Group:    group,
			Consumer: "dead-consumer",
			Streams:  []string{stream, ">"},
			Count:    int64(count),
			Block:    -1,
		},
	).Result()

	require.NoError(t, err)
	require.Len(t, streams, 1)
	require.Len(t, streams[0].Messages, count)

	require.EqualValues(
		t,
		count,
		PendingCount(t, client, stream, group),
	)

	return ids
}

// PendingCount возвращает количество сообщений в PEL группы.
func PendingCount(
	t *testing.T,
	client *redis.Client,
	stream string,
	group string,
) int64 {
	t.Helper()

	pending, err := client.XPending(
		context.Background(),
		stream,
		group,
	).Result()

	require.NoError(t, err)

	return pending.Count
}
