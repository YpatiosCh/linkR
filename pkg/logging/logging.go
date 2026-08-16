// Package logging constructs the application's structured logger.
package logging

import (
	"log/slog"
	"os"
	"strings"
)

// New constructs the application's structured logger. Output format is
// selected by appEnv: JSON on stdout in production, so container log
// drivers/log aggregators can parse it directly, and human-readable text
// on stdout otherwise, for local development. The minimum level is parsed
// from levelName ("debug", "info", "warn"/"warning", "error",
// case-insensitive); an empty or unrecognized value defaults to Info, so a
// typo in LOG_LEVEL never prevents the server from starting.
func New(appEnv, levelName string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLevel(levelName)}

	var handler slog.Handler
	if appEnv == "production" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	return slog.New(handler)
}

// parseLevel maps a level name to its slog.Level, defaulting to Info for
// any empty or unrecognized value.
func parseLevel(levelName string) slog.Level {
	switch strings.ToLower(levelName) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
