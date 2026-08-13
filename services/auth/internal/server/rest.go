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
)

func RunREST(addr string, authHandler *handler.AuthHandler) (func(context.Context) error, error) {
	r := chi.NewRouter()

	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)

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
