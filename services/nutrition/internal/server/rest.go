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
)

func RunREST(
	addr string,
	readinessChecks ...ReadinessCheck,
) (func(context.Context) error, error) {
	r := chi.NewRouter()

	registry, httpMetrics := appmetrics.NewServiceRegistry()

	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(middleware.HTTPTracing("nutrition"))
	r.Use(chimiddleware.Logger)
	r.Use(middleware.HTTPMetrics(httpMetrics))
	r.Use(chimiddleware.Recoverer)

	r.Get("/health/live", liveHandler)
	r.Get("/health/ready", readyHandler(readinessChecks...))

	r.Handle(
		"/metrics",
		promhttp.HandlerFor(
			registry,
			promhttp.HandlerOpts{},
		),
	)

	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
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
