package middleware

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// HTTPTracing — middleware для трассировки HTTP-запросов с использованием OpenTelemetry.
// Он использует otelhttp.NewMiddleware для создания middleware, который автоматически создает спаны для входящих HTTP-запросов.
// Middleware фильтрует запросы, исключая /metrics и /health/*, чтобы не создавать спаны для этих эндпоинтов.
// Для каждого запроса middleware извлекает маршрут из контекста chi и устанавливает его в качестве имени спана и атрибута http.route.
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
