package shutdown

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fitness-platform/pkg/logger"
)

func Graceful(ctx context.Context, cancel context.CancelFunc, servers ...func(ctx context.Context) error) {
	quit := make(chan os.Signal, 1)

	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	sig := <-quit
	logger.Log.Info().Str("signal", sig.String()).Msg("Shutdown signal received")

	cancel()

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()

	for _, srv := range servers {
		if err := srv(shutdownCtx); err != nil {
			logger.Log.Error().Err(err).Msg("server forced to shutdown")
		}
	}

	logger.Log.Info().Msg("All servers stopped gracefully")
}
