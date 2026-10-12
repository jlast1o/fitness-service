package shutdown

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fitness-platform/pkg/logger"
)

// Общий бюджет для завершения HTTP и фоновой работы.
const DefaultTimeout = 10 * time.Second

// GracefulDrain ожидает сигнал остановки,
// после чего запускает двухфазное завершение.
func GracefulDrain(
	stopPolling context.CancelFunc,
	cancelWork context.CancelFunc,
	consumerDone <-chan struct{},
	httpShutdown func(context.Context) error,
	tracingShutdown func(context.Context) error,
) error {
	// Подписываемся на SIGINT и SIGTERM.
	signalCtx, stopSignals := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stopSignals()

	// Ждём сигнала остановки.
	<-signalCtx.Done()

	logger.Log.Info().
		Msg("shutdown signal received")

	// Один общий deadline для всего завершения.
	shutdownCtx, cancelDeadline := context.WithTimeout(
		context.Background(),
		DefaultTimeout,
	)
	defer cancelDeadline()

	err := Drain(
		shutdownCtx,
		stopPolling,
		cancelWork,
		consumerDone,
		httpShutdown,
		tracingShutdown,
	)

	if err != nil {
		logger.Log.Error().
			Err(err).
			Msg("graceful shutdown incomplete")
	} else {
		logger.Log.Info().
			Msg("graceful shutdown completed")
	}

	return err
}

// Drain реализует завершение независимо от сигналов ОС.
// Это позволяет тестировать его напрямую.
func Drain(
	ctx context.Context,
	stopPolling context.CancelFunc,
	cancelWork context.CancelFunc,
	consumerDone <-chan struct{},
	httpShutdown func(context.Context) error,
	tracingShutdown func(context.Context) error,
) error {
	// Сначала прекращаем получение новой работы.
	stopPolling()

	// Страховка: при любом выходе из Drain
	// контекст активной работы будет отменён.
	defer cancelWork()

	// HTTP и consumer завершаются параллельно.
	httpDone := make(chan error, 1)

	if httpShutdown != nil {
		go func() {
			httpDone <- httpShutdown(ctx)
		}()
	} else {
		close(httpDone)
	}

	var httpErr error

	// Ждём завершения обоих компонентов.
	for consumerDone != nil || httpDone != nil {
		select {
		case <-consumerDone:
			consumerDone = nil

		case err, ok := <-httpDone:
			if ok {
				httpErr = err
			}
			httpDone = nil

		case <-ctx.Done():
			// Deadline истёк.
			// Просим активную работу завершиться.
			cancelWork()

			return errors.Join(
				httpErr,
				ctx.Err(),
			)
		}
	}

	// Обработка завершена, workCtx больше не нужен.
	cancelWork()

	// Теперь можно сбрасывать буферы трассировки.
	if tracingShutdown != nil {
		return errors.Join(
			httpErr,
			tracingShutdown(ctx),
		)
	}

	return httpErr
}
