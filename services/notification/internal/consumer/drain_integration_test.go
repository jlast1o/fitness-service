package consumer

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"fitness-platform/pkg/testutil/redisstreamtest"
)

type drainSender struct {
	started chan struct{}
	release chan struct{}
}

func (s *drainSender) Send(
	ctx context.Context,
	_, _ string,
) error {
	close(s.started)

	select {
	case <-ctx.Done():
		return ctx.Err()

	case <-s.release:
		return nil
	}
}

func TestNotificationDrain_FinishesSendAndAckAfterPollingStops(
	t *testing.T,
) {
	client := redisstreamtest.NewClient(t)
	ctx := context.Background()

	const (
		stream = "notification.drain.stream"
		group  = "notification.drain.group"
	)

	if err := client.XGroupCreateMkStream(
		ctx, stream, group, "0",
	).Err(); err != nil {
		t.Fatal(err)
	}

	if err := client.XAdd(
		ctx,
		&redis.XAddArgs{
			Stream: stream,
			Values: map[string]any{
				"event": workoutCreatedEventJSON,
			},
		},
	).Err(); err != nil {
		t.Fatal(err)
	}

	streams, err := client.XReadGroup(
		ctx,
		&redis.XReadGroupArgs{
			Group:    group,
			Consumer: "notification-drain-1",
			Streams:  []string{stream, ">"},
			Count:    1,
		},
	).Result()

	if err != nil ||
		len(streams) != 1 ||
		len(streams[0].Messages) != 1 {
		t.Fatalf(
			"read test message: %v, streams=%d",
			err, len(streams),
		)
	}

	sender := &drainSender{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}

	c := NewRedisConsumer(
		client,
		stream,
		group,
		"notification-drain-1",
		time.Second,
		sender,
	)

	pollCtx, stopPolling := context.WithCancel(ctx)
	workCtx, cancelWork := context.WithCancel(ctx)
	defer stopPolling()
	defer cancelWork()

	done := make(chan struct{})

	go func() {
		defer close(done)

		c.processMessagesWithContexts(
			pollCtx,
			workCtx,
			streams[0].Messages,
		)
	}()

	// Sender уже обрабатывает сообщение.
	select {
	case <-sender.started:
	case <-time.After(3 * time.Second):
		t.Fatal("Send did not start")
	}

	// Останавливаем только polling.
	stopPolling()

	// Завершаем уже начатую отправку.
	close(sender.release)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Send did not complete")
	}

	pending, err := client.XPending(
		ctx, stream, group,
	).Result()

	if err != nil || pending.Count != 0 {
		t.Fatalf(
			"expected ACK to clear PEL, pending=%v, err=%v",
			pending, err,
		)
	}
}
