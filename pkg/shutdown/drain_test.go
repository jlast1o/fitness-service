package shutdown

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDrain_StopsPollingButFinishesActiveWork(
	t *testing.T,
) {
	pollCtx, stopPolling := context.WithCancel(
		context.Background(),
	)
	workCtx, cancelWork := context.WithCancel(
		context.Background(),
	)

	defer stopPolling()
	defer cancelWork()

	consumerDone := make(chan struct{})
	activeFinished := make(chan struct{})

	go func() {
		// Ждём остановки polling.
		<-pollCtx.Done()

		// Уже начатая работа должна получить
		// время на завершение.
		select {
		case <-workCtx.Done():

		case <-time.After(30 * time.Millisecond):
			close(activeFinished)
		}

		close(consumerDone)
	}()

	httpCalled := make(chan struct{})

	ctx, cancelDeadline := context.WithTimeout(
		context.Background(),
		time.Second,
	)
	defer cancelDeadline()

	err := Drain(
		ctx,
		stopPolling,
		cancelWork,
		consumerDone,

		func(ctx context.Context) error {
			close(httpCalled)
			return nil
		},

		func(ctx context.Context) error {
			select {
			case <-activeFinished:
			default:
				t.Error(
					"tracing closed before active work completed",
				)
			}
			return nil
		},
	)

	if err != nil {
		t.Fatal(err)
	}

	if pollCtx.Err() != context.Canceled {
		t.Fatal(
			"polling context should be cancelled",
		)
	}

	if workCtx.Err() != context.Canceled {
		t.Fatal(
			"work context should be cancelled after drain",
		)
	}

	select {
	case <-httpCalled:
	default:
		t.Fatal("HTTP shutdown was not called")
	}

	select {
	case <-activeFinished:
	default:
		t.Fatal(
			"in-flight work was not given time to finish",
		)
	}
}

func TestDrain_DeadlineCancelsWorkWithoutHanging(
	t *testing.T,
) {
	pollCtx, stopPolling := context.WithCancel(
		context.Background(),
	)
	workCtx, cancelWork := context.WithCancel(
		context.Background(),
	)

	defer stopPolling()
	defer cancelWork()

	// Имитируем consumer, который не завершился.
	neverDone := make(chan struct{})

	ctx, cancelDeadline := context.WithTimeout(
		context.Background(),
		40*time.Millisecond,
	)
	defer cancelDeadline()

	start := time.Now()

	err := Drain(
		ctx,
		stopPolling,
		cancelWork,
		neverDone,

		func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},

		nil,
	)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf(
			"expected deadline exceeded, got %v",
			err,
		)
	}

	if time.Since(start) > time.Second {
		t.Fatal("shutdown waited too long")
	}

	if pollCtx.Err() != context.Canceled {
		t.Fatal("polling not stopped")
	}

	if workCtx.Err() != context.Canceled {
		t.Fatal(
			"work not cancelled on deadline",
		)
	}
}

func TestDrain_WaitsForHTTPAndConsumerConcurrently(
	t *testing.T,
) {
	pollCtx, stopPolling := context.WithCancel(
		context.Background(),
	)
	_, cancelWork := context.WithCancel(
		context.Background(),
	)

	defer stopPolling()
	defer cancelWork()

	release := make(chan struct{})
	consumerStarted := make(chan struct{})
	httpStarted := make(chan struct{})
	consumerDone := make(chan struct{})

	go func() {
		<-pollCtx.Done()
		close(consumerStarted)

		<-release
		close(consumerDone)
	}()

	ctx, cancel := context.WithTimeout(
		context.Background(),
		time.Second,
	)
	defer cancel()

	drained := make(chan error, 1)

	go func() {
		drained <- Drain(
			ctx,
			stopPolling,
			cancelWork,
			consumerDone,

			func(ctx context.Context) error {
				close(httpStarted)
				<-release
				return nil
			},

			nil,
		)
	}()

	select {
	case <-consumerStarted:
	case <-time.After(500 * time.Millisecond):
		t.Fatal(
			"consumer shutdown did not start",
		)
	}

	select {
	case <-httpStarted:
	case <-time.After(500 * time.Millisecond):
		t.Fatal(
			"HTTP shutdown did not start concurrently",
		)
	}

	// Разрешаем завершиться сразу обоим.
	close(release)

	select {
	case err := <-drained:
		if err != nil {
			t.Fatal(err)
		}

	case <-time.After(time.Second):
		t.Fatal("drain did not complete")
	}
}
