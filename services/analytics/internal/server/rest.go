package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"fitness-platform/pkg/logger"
	"fitness-platform/pkg/middleware"
	"fitness-platform/services/analytics/internal/handler"
)

// RunREST запускает HTTP-сервер с chi роутером.
// Принимает адрес, обработчики и секрет для JWT.
// Возвращает функцию graceful shutdown.
func RunREST(addr string, analyticsHandler *handler.AnalyticsHandler, jwtSecret string) (func(context.Context) error, error) {
	r := chi.NewRouter()

	// Базовые middleware для всех маршрутов
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)

	// Защищённые маршруты аналитики
	r.Group(func(r chi.Router) {
		r.Use(middleware.JWTAuth(jwtSecret))

		r.Get("/analytics/dashboard", analyticsHandler.Dashboard)
		r.Get("/analytics/progress", analyticsHandler.Progress)
		r.Get("/analytics/history", analyticsHandler.WorkoutHistory)
	})

	// Настраиваем HTTP-сервер
	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Запускаем сервер в горутине
	go func() {
		logger.Log.Info().Str("addr", addr).Msg("Analytics HTTP server starting")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Log.Fatal().Err(err).Msg("Analytics HTTP server failed")
		}
	}()

	// Функция graceful shutdown
	shutdownFunc := func(ctx context.Context) error {
		logger.Log.Info().Msg("shutting down Analytics HTTP server")
		return srv.Shutdown(ctx)
	}

	return shutdownFunc, nil
}
