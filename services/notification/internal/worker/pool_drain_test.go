package worker_test

import (
	"context"
	"testing"
	"time"

	"fitness-platform/services/notification/internal/domain"
	"fitness-platform/services/notification/internal/worker"
)

type recordingSender struct {
	received chan string
}

func (s *recordingSender) Send(
	ctx context.Context,
	_ string,
	message string,
) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case s.received <- message:
		return nil
	}
}

func TestPoolShutdown_DrainsAcceptedTasks(t *testing.T) {
	sender := &recordingSender{
		received: make(chan string, 2),
	}

	pool := worker.NewPool(
		context.Background(),
		sender,
		1,
		2,
	)
	pool.Start()

	for _, message := range []string{"one", "two"} {
		ok := pool.Submit(domain.NotificationTask{
			UserID:  "u",
			Message: message,
		})
		if !ok {
			t.Fatalf(
				"rejected task %q before shutdown",
				message,
			)
		}
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		time.Second,
	)
	defer cancel()

	if err := pool.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	if len(sender.received) != 2 {
		t.Fatalf(
			"expected two completed sends, got %d",
			len(sender.received),
		)
	}

	if pool.Submit(domain.NotificationTask{
		UserID:  "u",
		Message: "late",
	}) {
		t.Fatal("accepted task after shutdown")
	}
}
