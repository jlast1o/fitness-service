package logger

import (
	"os"
	"time"

	"github.com/rs/zerolog"
)

var Log zerolog.Logger

func Init(levelStr string) {
	lvl, err := zerolog.ParseLevel(levelStr)

	if err != nil {
		lvl = zerolog.InfoLevel
	}

	output := zerolog.ConsoleWriter{
		Out:        os.Stderr,
		TimeFormat: time.RFC3339,
	}

	Log = zerolog.New(output).
		Level(lvl).With().Timestamp().Caller().Logger()
}
