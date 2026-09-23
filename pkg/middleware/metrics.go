package middleware

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	appmetrics "fitness-platform/pkg/metrics"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

func HTTPMetrics(metrics *appmetrics.HTTPMetrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Не считаем технический observability-трафик
			// как пользовательские HTTP-запросы.
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

			statusCode := strconv.Itoa(status)

			metrics.Requests.WithLabelValues(
				r.Method,
				route,
				statusCode,
			).Inc()

			metrics.Duration.WithLabelValues(
				r.Method,
				route,
				statusCode,
			).Observe(time.Since(start).Seconds())
		})
	}
}
