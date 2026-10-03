package server

import (
	"context"
	"net/http"
)

type ReadinessCheck func(context.Context) error

func liveHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func readyHandler(checks ...ReadinessCheck) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		for _, check := range checks {
			if err := check(r.Context()); err != nil {
				http.Error(
					w,
					"service not ready",
					http.StatusServiceUnavailable,
				)
				return
			}
		}

		w.WriteHeader(http.StatusOK)
	}
}
