package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const (
	testLivePath  = "/health/live"
	testReadyPath = "/health/ready"
)

func TestLiveHandler_OK(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, testLivePath, nil)
	rec := httptest.NewRecorder()

	liveHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestReadyHandler_DatabaseAvailable(t *testing.T) {
	dbCheck := func(context.Context) error {
		return nil
	}

	handler := readyHandler(time.Second, dbCheck)

	req := httptest.NewRequest(http.MethodGet, testReadyPath, nil)
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestReadyHandler_DatabaseUnavailable(t *testing.T) {
	dbCheck := func(context.Context) error {
		return errors.New("database unavailable")
	}

	handler := readyHandler(time.Second, dbCheck)

	req := httptest.NewRequest(http.MethodGet, testReadyPath, nil)
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusServiceUnavailable,
			rec.Code,
		)
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

	req := httptest.NewRequest(http.MethodGet, testReadyPath, nil)
	rec := httptest.NewRecorder()

	start := time.Now()
	handler(rec, req)
	elapsed := time.Since(start)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusServiceUnavailable,
			rec.Code,
		)
	}

	if elapsed > maxExpectedTime {
		t.Fatalf(
			"readiness check exceeded expected timeout: elapsed=%s, max=%s",
			elapsed,
			maxExpectedTime,
		)
	}
}
