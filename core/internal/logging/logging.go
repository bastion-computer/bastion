// Package logging configures Bastion structured loggers.
package logging

import (
	"errors"
	"io"
	"log/slog"
	"strings"
)

const (
	// DefaultFormat is the default log handler format.
	DefaultFormat = "json"

	// DefaultLevel is the default minimum log level.
	DefaultLevel = "info"

	textFormat = "text"
)

// New returns a slog logger configured with the requested format and level.
func New(w io.Writer, format, level string) (*slog.Logger, error) {
	parsedLevel, err := parseLevel(level)
	if err != nil {
		return nil, err
	}

	options := &slog.HandlerOptions{Level: parsedLevel}

	switch normalize(format, DefaultFormat) {
	case DefaultFormat:
		return slog.New(slog.NewJSONHandler(w, options)), nil
	case textFormat:
		return slog.New(slog.NewTextHandler(w, options)), nil
	default:
		return nil, errors.New("log format must be one of json or text")
	}
}

func parseLevel(level string) (slog.Level, error) {
	switch normalize(level, DefaultLevel) {
	case "debug":
		return slog.LevelDebug, nil
	case DefaultLevel:
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, errors.New("log level must be one of debug, info, warn, or error")
	}
}

func normalize(value, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return fallback
	}

	return value
}
