package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const (
	healthOKStatus      = 200
	healthFailStatus    = 503
	healthLiveEndpoint  = "/health/live"
	healthReadyEndpoint = "/health/ready"
)

func TestLiveHandler_OK(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, healthLiveEndpoint, nil)
	rec := httptest.NewRecorder()

	liveHandler(rec, req)

	if rec.Code != healthOKStatus {
		t.Fatalf("expected status %d, got %d", healthOKStatus, rec.Code)
	}
}

func TestReadyHandler_DatabaseAvailable(t *testing.T) {
	checkDB := func(context.Context) error {
		return nil
	}

	handler := readyHandler(time.Second, checkDB)

	req := httptest.NewRequest(http.MethodGet, healthReadyEndpoint, nil)
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != healthOKStatus {
		t.Fatalf("expected status %d, got %d", healthOKStatus, rec.Code)
	}
}

func TestReadyHandler_DatabaseUnavailable(t *testing.T) {
	checkDB := func(context.Context) error {
		return context.Canceled
	}

	handler := readyHandler(time.Second, checkDB)

	req := httptest.NewRequest(http.MethodGet, healthReadyEndpoint, nil)
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != healthFailStatus {
		t.Fatalf("expected status %d, got %d", healthFailStatus, rec.Code)
	}
}

func TestReadyHandler_CheckTimeout(t *testing.T) {
	check := func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}

	readinessTimeout := 50 * time.Millisecond
	maxExpectedTime := 250 * time.Millisecond

	handler := readyHandler(readinessTimeout, check)

	req := httptest.NewRequest(http.MethodGet, healthReadyEndpoint, nil)
	rec := httptest.NewRecorder()

	start := time.Now()
	handler(rec, req)
	elapsed := time.Since(start)

	if rec.Code != healthFailStatus {
		t.Fatalf("expected status %d, got %d", healthFailStatus, rec.Code)
	}

	if elapsed > maxExpectedTime {
		t.Fatalf(
			"readiness check exceeded expected timeout: elapsed=%s, max=%s",
			elapsed,
			maxExpectedTime,
		)
	}
}
