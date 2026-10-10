package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"fitness-platform/pkg/logger"
	appmetrics "fitness-platform/pkg/metrics"
	"fitness-platform/pkg/middleware"
	"fitness-platform/services/nutrition/internal/handler"
)

// NewRouter создаёт HTTP-маршруты Nutrition Service.
// Используется и приложением, и тестами.
func NewRouter(
	nutritionHandler *handler.NutritionHandler,
	jwtSecret string,
	readinessTimeout time.Duration,
	readinessChecks ...ReadinessCheck,
) http.Handler {
	r := chi.NewRouter()

	registry, httpMetrics := appmetrics.NewServiceRegistry()

	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)

	r.Use(middleware.HTTPTracing("nutrition"))
	r.Use(middleware.HTTPLogging)
	r.Use(middleware.HTTPMetrics(httpMetrics))
	r.Use(chimiddleware.Recoverer)

	r.Get("/health/live", liveHandler)

	r.Get(
		"/health/ready",
		readyHandler(readinessTimeout, readinessChecks...),
	)

	r.Handle(
		"/metrics",
		promhttp.HandlerFor(
			registry,
			promhttp.HandlerOpts{},
		),
	)

	// Защищённые маршруты.
	r.Group(func(r chi.Router) {
		r.Use(middleware.JWTAuth(jwtSecret))

		r.Post(
			"/nutrition/profile",
			nutritionHandler.UpsertProfile,
		)

		r.Get(
			"/nutrition/profile",
			nutritionHandler.GetProfile,
		)

		r.Get(
			"/nutrition/targets",
			nutritionHandler.GetTargets,
		)
	})

	return r
}

// RunREST запускает HTTP-сервер.
func RunREST(
	addr string,
	nutritionHandler *handler.NutritionHandler,
	jwtSecret string,
	readinessTimeout time.Duration,
	readinessChecks ...ReadinessCheck,
) (func(context.Context) error, error) {

	router := NewRouter(
		nutritionHandler,
		jwtSecret,
		readinessTimeout,
		readinessChecks...,
	)

	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Log.Info().
			Str("addr", addr).
			Msg("Nutrition HTTP server starting")

		if err := srv.ListenAndServe(); err != nil &&
			!errors.Is(err, http.ErrServerClosed) {
			logger.Log.Fatal().
				Err(err).
				Msg("Nutrition HTTP server failed")
		}
	}()

	shutdownFunc := func(ctx context.Context) error {
		logger.Log.Info().
			Msg("shutting down Nutrition HTTP server")

		return srv.Shutdown(ctx)
	}

	return shutdownFunc, nil
}
