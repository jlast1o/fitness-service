package outbox

import (
	"context"
	"sync"
	"testing"
	"time"

	"fitness-platform/pkg/testutil/redisstreamtest"
	"fitness-platform/services/workout/internal/domain"
)

type blockingMarkRepo struct {
	mu          sync.Mutex
	claimed     bool
	markStarted chan struct{}
	finishMark  chan struct{}
	marked      bool
}

func (r *blockingMarkRepo) ClaimNextOutboxEvent(
	ctx context.Context,
	claimToken string,
	lease time.Duration,
) (*domain.OutboxEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.claimed {
		return nil, nil
	}

	r.claimed = true

	return &domain.OutboxEvent{
		ID:           "11111111-1111-4111-8111-111111111111",
		EventType:    "workout.created",
		EventVersion: 1,
		Payload:      map[string]any{"user_id": "u"},
		CreatedAt:    time.Now(),
	}, nil
}

func (r *blockingMarkRepo) MarkOutboxEventPublishedClaimed(
	ctx context.Context,
	eventID string,
	claimToken string,
) error {
	close(r.markStarted)

	select {
	case <-ctx.Done():
		return ctx.Err()

	case <-r.finishMark:
		r.marked = true
		return nil
	}
}

func TestPublisherDrain_CompletesClaimedEventAfterPollingStops(
	t *testing.T,
) {
	client := redisstreamtest.NewClient(t)

	repo := &blockingMarkRepo{
		markStarted: make(chan struct{}),
		finishMark:  make(chan struct{}),
	}

	publisher := NewPublisher(
		repo,
		client,
		"publisher.drain.test",
		time.Millisecond,
	)

	rootCtx := context.Background()

	pollCtx, stopPolling := context.WithCancel(rootCtx)
	workCtx, cancelWork := context.WithCancel(rootCtx)
	defer stopPolling()
	defer cancelWork()

	done := make(chan struct{})

	go func() {
		defer close(done)
		publisher.RunWithContexts(pollCtx, workCtx)
	}()

	// Дожидаемся, пока XADD уже выполнен,
	// а MarkPublished ещё не завершился.
	select {
	case <-repo.markStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("publisher never reached MarkPublished")
	}

	// Имитируем SIGTERM: новых событий не берём.
	stopPolling()

	// Но текущему MarkPublished даём закончиться.
	close(repo.finishMark)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("publisher did not stop")
	}

	if !repo.marked {
		t.Fatal("MarkPublished did not finish")
	}

	length, err := client.XLen(
		context.Background(),
		"publisher.drain.test",
	).Result()

	if err != nil || length != 1 {
		t.Fatalf(
			"expected 1 published event, got %d, err %v",
			length, err,
		)
	}
}
