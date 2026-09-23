package server

import (
	"context"
	"errors"
	"fitness-platform/pkg/logger"
	"fitness-platform/services/auth/internal/handler"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/rs/cors"

	appmetrics "fitness-platform/pkg/metrics"
	appmiddleware "fitness-platform/pkg/middleware"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func RunREST(
	addr string,
	authHandler *handler.AuthHandler,
	readinessChecks ...ReadinessCheck,
) (func(context.Context) error, error) {
	r := chi.NewRouter()

	registry, httpMetrics := appmetrics.NewServiceRegistry()

	r.Use(cors.New(cors.Options{
		AllowedOrigins:   []string{"http://localhost:3000"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
	}).Handler)
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.Logger)
	r.Use(appmiddleware.HTTPMetrics(httpMetrics))
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

	r.Post("/auth/register", authHandler.Register)
	r.Post("/auth/login", authHandler.Login)

	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Log.Info().Str("addr", addr).Msg("HTTP server starting")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Log.Fatal().Err(err).Msg("http server failed")
		}
	}()

	shutdownFunc := func(ctx context.Context) error {
		logger.Log.Info().Msg("shutting down HTTP server")
		return srv.Shutdown(ctx)
	}

	return shutdownFunc, nil

}
