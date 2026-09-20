package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
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

	handler := readyHandler(dbCheck)

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

	handler := readyHandler(dbCheck)

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
