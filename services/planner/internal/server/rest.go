package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/rs/cors"

	"fitness-platform/pkg/logger"
	"fitness-platform/pkg/middleware"
	"fitness-platform/services/planner/internal/handler"
)

// RunREST запускает HTTP-сервер с chi роутером.
func RunREST(addr string, plannerHandler *handler.PlannerHandler, jwtSecret string) (func(context.Context) error, error) {
	r := chi.NewRouter()
	r.Use(cors.New(cors.Options{
		AllowedOrigins:   []string{"http://localhost:3000"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
	}).Handler)
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)

	r.Group(func(r chi.Router) {
		r.Use(middleware.JWTAuth(jwtSecret))

		r.Post("/planner/profile", plannerHandler.UpsertProfile)
		r.Get("/planner/profile", plannerHandler.GetProfile)
		r.Get("/planner/exercises", plannerHandler.ListExercises)
		r.Post("/planner/plans", plannerHandler.GeneratePlan)
		r.Get("/planner/plans/current", plannerHandler.GetCurrentPlan)
		r.Get("/planner/next-workout", plannerHandler.GetNextWorkout)
	})

	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Log.Info().Str("addr", addr).Msg("Planner HTTP server starting")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Log.Fatal().Err(err).Msg("Planner HTTP server failed")
		}
	}()

	shutdownFunc := func(ctx context.Context) error {
		logger.Log.Info().Msg("shutting down Planner HTTP server")
		return srv.Shutdown(ctx)
	}

	return shutdownFunc, nil
}
