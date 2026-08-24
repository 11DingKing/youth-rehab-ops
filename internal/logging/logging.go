package logging

import (
	"io"
	"log/slog"
)

func New(writer io.Writer, level slog.Level) *slog.Logger {
	handler := slog.NewJSONHandler(writer, &slog.HandlerOptions{Level: level})
	return slog.New(handler).With("service", "youth-rehab-ops")
}
