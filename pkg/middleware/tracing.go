package middleware

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func HTTPTracing(serviceName string) func(http.Handler) http.Handler {
	otelMiddleware := otelhttp.NewMiddleware(
		serviceName+".http",
		otelhttp.WithFilter(func(r *http.Request) bool {
			return r.URL.Path != "/metrics" &&
				!strings.HasPrefix(r.URL.Path, "/health/")
		}),
	)

	return func(next http.Handler) http.Handler {
		routeAwareHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)

			route := chi.RouteContext(r.Context()).RoutePattern()
			if route == "" {
				return
			}

			span := trace.SpanFromContext(r.Context())
			if !span.IsRecording() {
				return
			}

			span.SetName(r.Method + " " + route)
			span.SetAttributes(
				attribute.String("http.route", route),
			)
		})

		return otelMiddleware(routeAwareHandler)
	}
}
