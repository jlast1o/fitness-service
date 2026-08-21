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
	"fitness-platform/services/workout/internal/handler"
)

// RunREST запускает HTTP-сервер с chi роутером.
// Принимает адрес, обработчики и секрет для JWT.
// Возвращает функцию graceful shutdown.
func RunREST(addr string, workoutHandler *handler.WorkoutHandler, jwtSecret string) (func(context.Context) error, error) {
	r := chi.NewRouter()
	r.Use(cors.New(cors.Options{
		AllowedOrigins:   []string{"http://localhost:3000"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
	}).Handler)
	// Базовые middleware для всех маршрутов
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)

	// Группа защищённых маршрутов
	r.Group(func(r chi.Router) {
		// Все запросы в этой группе проходят JWT-проверку
		r.Use(middleware.JWTAuth(jwtSecret))

		// Workouts
		r.Post("/workouts", workoutHandler.CreateWorkout)
		r.Get("/workouts", workoutHandler.ListWorkouts)
		r.Get("/workouts/{id}", workoutHandler.GetWorkout)
		r.Delete("/workouts/{id}", workoutHandler.DeleteWorkout)

		// Exercises (админские/пользовательские, но тоже требуют auth)
		r.Get("/exercises", workoutHandler.ListExercises)
		r.Post("/exercises", workoutHandler.CreateExercise)
	})

	// Настраиваем HTTP-сервер
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
			logger.Log.Fatal().Err(err).Msg("HTTP server failed")
		}
	}()

	shutdownFunc := func(ctx context.Context) error {
		logger.Log.Info().Msg("shutting down HTTP server")
		return srv.Shutdown(ctx)
	}

	return shutdownFunc, nil
}
