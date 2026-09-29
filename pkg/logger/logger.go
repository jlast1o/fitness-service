package logger

import (
	"context"
	"os"
	"time"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"
)

var Log zerolog.Logger

func Init(serviceName, levelStr string) {
	level, err := zerolog.ParseLevel(levelStr)
	if err != nil {
		level = zerolog.InfoLevel
	}

	zerolog.TimeFieldFormat = time.RFC3339Nano

	Log = zerolog.New(os.Stdout).
		Level(level).
		With().
		Timestamp().
		Str("service", serviceName).
		Caller().
		Logger()
}

// FromContext возвращает logger, обогащённый observability-контекстом.
//
// Для HTTP-запросов:
//   - request_id
//   - trace_id
//   - span_id
//
// Для background processing trace_id/span_id также появятся,
// если в ctx существует активный OpenTelemetry span.
func FromContext(ctx context.Context) *zerolog.Logger {
	logContext := Log.With()

	if requestID := chimiddleware.GetReqID(ctx); requestID != "" {
		logContext = logContext.Str("request_id", requestID)
	}

	spanContext := trace.SpanContextFromContext(ctx)
	if spanContext.IsValid() {
		logContext = logContext.
			Str("trace_id", spanContext.TraceID().String()).
			Str("span_id", spanContext.SpanID().String())
	}

	log := logContext.Logger()

	return &log
}
