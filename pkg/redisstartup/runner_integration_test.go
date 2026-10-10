package redisstartup

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"fitness-platform/pkg/testutil/redisstreamtest"
)

// TestEnsureGroup_CreatesAtBeginningAndIsIdempotent verifies that a new
// consumer group sees messages already present in the Stream.
func TestEnsureGroup_CreatesAtBeginningAndIsIdempotent(t *testing.T) {
	admin := redisstreamtest.NewClient(t)
	ctx := context.Background()
	const stream, group = "startup.ensure.stream", "startup.ensure.group"

	id, err := admin.XAdd(ctx, &redis.XAddArgs{
		Stream: stream,
		Values: map[string]any{"event": "preexisting"},
	}).Result()
	require.NoError(t, err)

	require.NoError(t, EnsureGroup(ctx, admin, stream, group))
	// Redis responds with BUSYGROUP, which must be treated as success.
	require.NoError(t, EnsureGroup(ctx, admin, stream, group))

	streams, err := admin.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group: group, Consumer: "ensure-test", Streams: []string{stream, ">"},
		Count: 1, Block: -1,
	}).Result()
	require.NoError(t, err)
	require.Len(t, streams, 1)
	require.Len(t, streams[0].Messages, 1)
	require.Equal(t, id, streams[0].Messages[0].ID)
}

// runnerForTest performs actual XAUTOCLAIM and XREADGROUP operations against
// Redis. It ACKs messages directly because this tests Runner, not business logic.
func runnerForTest(
	client *redis.Client,
	stream, group string,
	ready func(),
	acked *atomic.Int64,
) Runner {
	return Runner{
		Client:       client,
		Stream:       stream,
		Group:        group,
		Service:      "runner-integration-test",
		OnGroupReady: ready,
		RecoverPending: func(ctx context.Context) error {
			_, _, err := client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
				Stream:   stream,
				Group:    group,
				Consumer: "runner-test",
				MinIdle:  time.Hour,
				Start:    "0-0",
				Count:    10,
			}).Result()
			if err != nil {
				return fmt.Errorf("xautoclaim: %w", err)
			}
			return nil
		},
		ReadNew: func(ctx context.Context) error {
			streams, err := client.XReadGroup(ctx, &redis.XReadGroupArgs{
				Group:    group,
				Consumer: "runner-test",
				Streams:  []string{stream, ">"},
				Count:    10,
				Block:    100 * time.Millisecond,
			}).Result()
			if errors.Is(err, redis.Nil) {
				return nil
			}
			if err != nil {
				return fmt.Errorf("xreadgroup: %w", err)
			}
			for _, s := range streams {
				for _, message := range s.Messages {
					if err := client.XAck(ctx, stream, group, message.ID).Err(); err != nil {
						return fmt.Errorf("xack: %w", err)
					}
					acked.Add(1)
				}
			}
			return nil
		},
	}
}

func startRunner(t *testing.T, runCtx context.Context, cancel context.CancelFunc, runner Runner) <-chan struct{} {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		runner.Run(runCtx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("Runner did not stop within 3 seconds")
		}
	})
	return done
}

func createTestRedisUser(t *testing.T, admin *redis.Client, username string) *redis.Client {
	t.Helper()
	const password = "startup-test-password"
	ctx := context.Background()

	// The dedicated account can read/write Streams but cannot create groups.
	require.NoError(t, admin.Do(ctx,
		"ACL", "SETUSER", username,
		"on", ">"+password, "~*", "&*", "+@all", "-xgroup",
	).Err())
	t.Cleanup(func() {
		_ = admin.Do(context.Background(), "ACL", "DELUSER", username).Err()
	})

	client := redis.NewClient(&redis.Options{
		Addr:         admin.Options().Addr,
		Username:     username,
		Password:     password,
		MaxRetries:   -1,
		DialTimeout:  time.Second,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: time.Second,
	})
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, client.Ping(ctx).Err())
	return client
}

func TestRunner_RetriesGroupCreationAndReadsExistingEvents(t *testing.T) {
	admin := redisstreamtest.NewClient(t)
	ctx := context.Background()
	const stream, group, user = "startup.retry.stream", "startup.retry.group", "startup_retry_user"

	_, err := admin.XAdd(ctx, &redis.XAddArgs{
		Stream: stream, Values: map[string]any{"event": "queued-before-startup"},
	}).Result()
	require.NoError(t, err)

	client := createTestRedisUser(t, admin, user)
	// Verify our fault injection really prevents creation.
	err = client.XGroupCreateMkStream(ctx, stream, group, "0").Err()
	require.ErrorContains(t, err, "NOPERM")

	var acked atomic.Int64
	runCtx, cancel := context.WithCancel(ctx)
	done := startRunner(t, runCtx, cancel, runnerForTest(client, stream, group, nil, &acked))

	// The consumer must stay alive through startup failures.
	select {
	case <-done:
		t.Fatal("Runner returned instead of retrying failed group creation")
	case <-time.After(350 * time.Millisecond):
	}

	// Redis permissions recover without restarting Runner.
	require.NoError(t, admin.Do(ctx, "ACL", "SETUSER", user, "+xgroup").Err())
	require.Eventually(t, func() bool {
		return acked.Load() == 1
	}, 8*time.Second, 25*time.Millisecond,
		"Runner must create the group and consume the message queued before startup")
}

func TestRunner_RecreatesGroupAfterNOGROUP(t *testing.T) {
	admin := redisstreamtest.NewClient(t)
	ctx := context.Background()
	const stream, group = "startup.recreate.stream", "startup.recreate.group"
	require.NoError(t, EnsureGroup(ctx, admin, stream, group))

	var readyCount, acked atomic.Int64
	runCtx, cancel := context.WithCancel(ctx)
	startRunner(t, runCtx, cancel, runnerForTest(admin, stream, group, func() {
		readyCount.Add(1)
	}, &acked))
	require.Eventually(t, func() bool { return readyCount.Load() == 1 },
		3*time.Second, 10*time.Millisecond)

	// Simulate losing the consumer group while Redis is running.
	deleted, err := admin.XGroupDestroy(ctx, stream, group).Result()
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted, "consumer group must be destroyed")

	_, err = admin.XAdd(ctx, &redis.XAddArgs{
		Stream: stream, Values: map[string]any{"event": "queued-while-group-missing"},
	}).Result()
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return readyCount.Load() >= 2 && acked.Load() >= 1
	}, 8*time.Second, 25*time.Millisecond,
		"Runner must detect NOGROUP, recreate group and consume existing stream messages")
}

func TestRunner_StopsPromptlyDuringStartupRetry(t *testing.T) {
	admin := redisstreamtest.NewClient(t)
	ctx := context.Background()
	const stream, group, user = "startup.cancel.stream", "startup.cancel.group", "startup_cancel_user"
	client := createTestRedisUser(t, admin, user)

	seenFailure := make(chan struct{})
	client.AddHook(&groupFailureHook{seen: seenFailure})

	var acked atomic.Int64
	runCtx, cancel := context.WithCancel(ctx)
	done := startRunner(t, runCtx, cancel, runnerForTest(client, stream, group, nil, &acked))

	select {
	case <-seenFailure:
	case <-time.After(3 * time.Second):
		t.Fatal("did not observe failed XGROUP CREATE")
	}

	start := time.Now()
	cancel()
	select {
	case <-done:
		require.Less(t, time.Since(start), time.Second)
	case <-time.After(time.Second):
		t.Fatal("Runner did not stop promptly during backoff")
	}
}

type groupFailureHook struct {
	seen chan struct{}
	once sync.Once
}

func (h *groupFailureHook) DialHook(next redis.DialHook) redis.DialHook {
	return next
}
func (h *groupFailureHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		err := next(ctx, cmd)
		if err != nil && strings.HasPrefix(cmd.Name(), "xgroup") {
			h.once.Do(func() { close(h.seen) })
		}
		return err
	}
}
func (h *groupFailureHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

func TestIsNoGroup_UnwrapsErrors(t *testing.T) {
	admin := redisstreamtest.NewClient(t)
	ctx := context.Background()
	const stream, group = "startup.nogroup.stream", "missing-group"
	_, err := admin.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    group,
		Consumer: "test",
		Streams:  []string{stream, ">"},
		Block:    -1,
	}).Result()
	require.Error(t, err)
	require.True(t, IsNoGroup(fmt.Errorf("wrapped XREADGROUP: %w", err)))
	require.False(t, IsNoGroup(errors.New("temporary connection error")))
}
