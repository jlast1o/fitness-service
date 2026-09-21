package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
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

func TestReadyHandler_AllChecksAvailable(t *testing.T) {
	checkDB := func(ctx context.Context) error {
		return nil
	}

	checkRedis := func(ctx context.Context) error {
		return nil
	}

	handler := readyHandler(checkDB, checkRedis)

	req := httptest.NewRequest(http.MethodGet, healthReadyEndpoint, nil)
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != healthOKStatus {
		t.Fatalf("expected status %d, got %d", healthOKStatus, rec.Code)
	}
}

func TestReadyHandler_CheckFailed(t *testing.T) {
	checkDB := func(ctx context.Context) error {
		return context.Canceled
	}

	handler := readyHandler(checkDB)

	req := httptest.NewRequest(http.MethodGet, healthReadyEndpoint, nil)
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != healthFailStatus {
		t.Fatalf("expected status %d, got %d", healthFailStatus, rec.Code)
	}
}
