package logger

import (
	"log/slog"
	"os"
	"strings"
)

func New(level string) *slog.Logger {
	lvl := slog.LevelInfo
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	}

	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: lvl,
	}))
}
