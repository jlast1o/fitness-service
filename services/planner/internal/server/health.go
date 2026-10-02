package server

import (
	"context"
	"net/http"
	"time"
)

type ReadinessCheck func(context.Context) error

func liveHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func readyHandler(
	timeout time.Duration,
	checks ...ReadinessCheck,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		if timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}

		for _, check := range checks {
			if err := check(ctx); err != nil {
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
