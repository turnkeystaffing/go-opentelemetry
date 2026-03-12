package opentelemetry

import (
	"context"
	"log/slog"
)

// MultiHandler fans out log records to multiple slog.Handler instances.
// A record is forwarded to each handler that reports Enabled for the record's level.
// Used to write logs to both stdout and OTel simultaneously.
//
// Usage:
//
//	handler := opentelemetry.NewMultiHandler(stdoutHandler, otelHandler)
//	logger := slog.New(handler)
type MultiHandler struct {
	handlers []slog.Handler
}

// NewMultiHandler creates a handler that fans out to all provided handlers.
func NewMultiHandler(handlers ...slog.Handler) *MultiHandler {
	return &MultiHandler{handlers: handlers}
}

func (h *MultiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h *MultiHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, r.Level) {
			if err := handler.Handle(ctx, r); err != nil {
				return err
			}
		}
	}
	return nil
}

func (h *MultiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	handlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		handlers[i] = handler.WithAttrs(attrs)
	}
	return &MultiHandler{handlers: handlers}
}

func (h *MultiHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	handlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		handlers[i] = handler.WithGroup(name)
	}
	return &MultiHandler{handlers: handlers}
}

var _ slog.Handler = (*MultiHandler)(nil)

// LevelFilterHandler wraps an slog.Handler that does not support level filtering
// (e.g., otelslog.NewHandler emits all levels) and enforces a minimum log level.
// Records below the configured level are rejected before reaching the inner handler.
//
// Usage:
//
//	filtered := opentelemetry.NewLevelFilterHandler(slog.LevelInfo, otelslogHandler)
type LevelFilterHandler struct {
	level slog.Leveler
	inner slog.Handler
}

// NewLevelFilterHandler wraps inner with minimum level filtering.
func NewLevelFilterHandler(level slog.Leveler, inner slog.Handler) *LevelFilterHandler {
	if inner == nil {
		panic("NewLevelFilterHandler: inner handler cannot be nil")
	}
	if level == nil {
		panic("NewLevelFilterHandler: level cannot be nil")
	}
	return &LevelFilterHandler{level: level, inner: inner}
}

func (h *LevelFilterHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

func (h *LevelFilterHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level < h.level.Level() {
		return nil
	}
	return h.inner.Handle(ctx, r)
}

func (h *LevelFilterHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	return &LevelFilterHandler{level: h.level, inner: h.inner.WithAttrs(attrs)}
}

func (h *LevelFilterHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &LevelFilterHandler{level: h.level, inner: h.inner.WithGroup(name)}
}

var _ slog.Handler = (*LevelFilterHandler)(nil)
