// Package obs provides structured logging (slog), Prometheus metrics, and
// OpenTelemetry tracing setup.
package obs

import (
	"log/slog"
	"os"
	"strings"
)

// LogLevels maps string levels to slog levels.
var logLevels = map[string]slog.Level{
	"debug": slog.LevelDebug,
	"info":  slog.LevelInfo,
	"warn":  slog.LevelWarn,
	"error": slog.LevelError,
}

// InitLogger initialises the global structured logger.
// level can be "debug", "info", "warn", "error".
// format can be "json" or "text".
func InitLogger(level, format string) *slog.Logger {
	opts := &slog.HandlerOptions{
		Level: logLevelFromString(level),
		AddSource: strings.ToLower(level) == "debug",
	}

	var handler slog.Handler
	if strings.ToLower(format) == "text" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger
}

func logLevelFromString(s string) slog.Level {
	if lvl, ok := logLevels[strings.ToLower(s)]; ok {
		return lvl
	}
	return slog.LevelInfo
}

// Logger returns the default logger with a component key.
func Logger(component string) *slog.Logger {
	return slog.Default().With("component", component)
}
