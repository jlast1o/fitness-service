package middleware

import (
	"net/http"
	"strings"
	"time"

	"fitness-platform/pkg/logger"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

func HTTPLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Не засоряем логи постоянными техническими probes/scrapes.
		if r.URL.Path == "/metrics" ||
			strings.HasPrefix(r.URL.Path, "/health/") {
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()

		ww := chimiddleware.NewWrapResponseWriter(
			w,
			r.ProtoMajor,
		)

		next.ServeHTTP(ww, r)

		route := chi.RouteContext(r.Context()).RoutePattern()
		if route == "" {
			route = "unknown"
		}

		status := ww.Status()
		if status == 0 {
			status = http.StatusOK
		}

		log := logger.FromContext(r.Context())

		event := log.Info()
		if status >= http.StatusInternalServerError {
			event = log.Error()
		}

		event.
			Str("method", r.Method).
			Str("route", route).
			Str("path", r.URL.Path).
			Int("status_code", status).
			Str("remote_addr", r.RemoteAddr).
			Float64(
				"duration_ms",
				float64(time.Since(start).Microseconds())/1000,
			).
			Msg("HTTP request completed")
	})
}
