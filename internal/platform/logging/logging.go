// Package logging builds the structured logger shared by every service.
package logging

import (
	"log/slog"
	"os"
	"strings"
)

// New returns a slog logger tagged with the service name.
//
// OBRABI_LOG_FORMAT selects "json" (default, good for docker logs) or "text"
// (nicer while developing); OBRABI_LOG_LEVEL selects debug/info/warn/error.
func New(service string) *slog.Logger {
	level := slog.LevelInfo
	switch strings.ToLower(os.Getenv("OBRABI_LOG_LEVEL")) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if strings.EqualFold(os.Getenv("OBRABI_LOG_FORMAT"), "text") {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}
	logger := slog.New(handler).With("service", service)
	slog.SetDefault(logger)
	return logger
}
