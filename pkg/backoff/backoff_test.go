package backoff

import (
	"context"
	"testing"
	"time"
)

func TestExponentialIncreasesAndCaps(t *testing.T) {
	b, err := New(
		200*time.Millisecond,
		2*time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}

	caps := []time.Duration{
		200 * time.Millisecond,
		400 * time.Millisecond,
		800 * time.Millisecond,
		1600 * time.Millisecond,
		2 * time.Second,
		2 * time.Second,
	}

	for i, cap := range caps {
		delay := b.Next()

		if delay < cap/2 || delay > cap {
			t.Fatalf(
				"attempt %d: got %v, want between %v and %v",
				i+1,
				delay,
				cap/2,
				cap,
			)
		}
	}
}

func TestReset(t *testing.T) {
	b, err := New(
		100*time.Millisecond,
		time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}

	b.Next()
	b.Next()

	b.Reset()

	delay := b.Next()

	if delay < 50*time.Millisecond ||
		delay > 100*time.Millisecond {
		t.Fatalf(
			"unexpected delay after reset: %v",
			delay,
		)
	}
}

func TestInvalidConfiguration(t *testing.T) {
	if _, err := New(0, time.Second); err == nil {
		t.Fatal("expected error for zero base")
	}

	if _, err := New(
		time.Second,
		500*time.Millisecond,
	); err == nil {
		t.Fatal("expected error for max smaller than base")
	}
}

func TestWaitReturnsWhenContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)

	cancel()

	err := Wait(ctx, time.Hour)

	if err != context.Canceled {
		t.Fatalf(
			"got %v, want context.Canceled",
			err,
		)
	}
}

func TestWaitReturnsAfterDelay(t *testing.T) {
	err := Wait(
		context.Background(),
		time.Millisecond,
	)

	if err != nil {
		t.Fatal(err)
	}
}
