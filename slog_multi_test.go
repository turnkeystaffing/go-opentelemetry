package opentelemetry

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// bufferHandler is a minimal slog.Handler that writes messages to a buffer for testing.
type bufferHandler struct {
	buf   *bytes.Buffer
	level slog.Level
	attrs []slog.Attr
	group string
}

func newBufferHandler(buf *bytes.Buffer, level slog.Level) *bufferHandler {
	return &bufferHandler{buf: buf, level: level}
}

func (h *bufferHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *bufferHandler) Handle(_ context.Context, r slog.Record) error {
	if h.group != "" {
		h.buf.WriteString(h.group + ".")
	}
	h.buf.WriteString(r.Message)
	for _, a := range h.attrs {
		h.buf.WriteString(" " + a.Key + "=" + a.Value.String())
	}
	h.buf.WriteString("\n")
	return nil
}

func (h *bufferHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &bufferHandler{buf: h.buf, level: h.level, attrs: append(h.attrs, attrs...), group: h.group}
}

func (h *bufferHandler) WithGroup(name string) slog.Handler {
	g := name
	if h.group != "" {
		g = h.group + "." + name
	}
	return &bufferHandler{buf: h.buf, level: h.level, attrs: h.attrs, group: g}
}

func TestMultiHandler_FanOut(t *testing.T) {
	var buf1, buf2 bytes.Buffer
	h1 := newBufferHandler(&buf1, slog.LevelInfo)
	h2 := newBufferHandler(&buf2, slog.LevelInfo)

	multi := NewMultiHandler(h1, h2)
	logger := slog.New(multi)

	logger.Info("hello")

	if !strings.Contains(buf1.String(), "hello") {
		t.Errorf("handler 1 missing message, got: %q", buf1.String())
	}
	if !strings.Contains(buf2.String(), "hello") {
		t.Errorf("handler 2 missing message, got: %q", buf2.String())
	}
}

func TestMultiHandler_LevelFiltering(t *testing.T) {
	var bufInfo, bufWarn bytes.Buffer
	hInfo := newBufferHandler(&bufInfo, slog.LevelInfo)
	hWarn := newBufferHandler(&bufWarn, slog.LevelWarn)

	multi := NewMultiHandler(hInfo, hWarn)
	logger := slog.New(multi)

	logger.Info("info-msg")
	logger.Warn("warn-msg")

	// Info handler should see both
	if !strings.Contains(bufInfo.String(), "info-msg") {
		t.Error("info handler should see info-msg")
	}
	if !strings.Contains(bufInfo.String(), "warn-msg") {
		t.Error("info handler should see warn-msg")
	}

	// Warn handler should only see warn
	if strings.Contains(bufWarn.String(), "info-msg") {
		t.Error("warn handler should not see info-msg")
	}
	if !strings.Contains(bufWarn.String(), "warn-msg") {
		t.Error("warn handler should see warn-msg")
	}
}

func TestMultiHandler_Enabled(t *testing.T) {
	var buf1, buf2 bytes.Buffer
	hDebug := newBufferHandler(&buf1, slog.LevelDebug)
	hError := newBufferHandler(&buf2, slog.LevelError)

	multi := NewMultiHandler(hDebug, hError)

	// Should be enabled if any handler is enabled
	if !multi.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("multi should be enabled at debug (hDebug accepts it)")
	}
	if !multi.Enabled(context.Background(), slog.LevelError) {
		t.Error("multi should be enabled at error")
	}
}

func TestMultiHandler_WithAttrs(t *testing.T) {
	var buf1, buf2 bytes.Buffer
	h1 := newBufferHandler(&buf1, slog.LevelInfo)
	h2 := newBufferHandler(&buf2, slog.LevelInfo)

	multi := NewMultiHandler(h1, h2)
	withAttr := multi.WithAttrs([]slog.Attr{slog.String("k", "v")})

	logger := slog.New(withAttr)
	logger.Info("test")

	if !strings.Contains(buf1.String(), "k=v") {
		t.Errorf("handler 1 missing attr, got: %q", buf1.String())
	}
	if !strings.Contains(buf2.String(), "k=v") {
		t.Errorf("handler 2 missing attr, got: %q", buf2.String())
	}
}

func TestMultiHandler_WithAttrsEmpty(t *testing.T) {
	var buf bytes.Buffer
	h := newBufferHandler(&buf, slog.LevelInfo)
	multi := NewMultiHandler(h)

	same := multi.WithAttrs(nil)
	if same != multi {
		t.Error("WithAttrs(nil) should return same handler")
	}
}

func TestMultiHandler_WithGroup(t *testing.T) {
	var buf1, buf2 bytes.Buffer
	h1 := newBufferHandler(&buf1, slog.LevelInfo)
	h2 := newBufferHandler(&buf2, slog.LevelInfo)

	multi := NewMultiHandler(h1, h2)
	grouped := multi.WithGroup("grp")

	logger := slog.New(grouped)
	logger.Info("test")

	if !strings.Contains(buf1.String(), "grp.test") {
		t.Errorf("handler 1 missing group prefix, got: %q", buf1.String())
	}
	if !strings.Contains(buf2.String(), "grp.test") {
		t.Errorf("handler 2 missing group prefix, got: %q", buf2.String())
	}
}

func TestMultiHandler_WithGroupEmpty(t *testing.T) {
	var buf bytes.Buffer
	h := newBufferHandler(&buf, slog.LevelInfo)
	multi := NewMultiHandler(h)

	same := multi.WithGroup("")
	if same != multi {
		t.Error("WithGroup(\"\") should return same handler")
	}
}

func TestLevelFilterHandler_FiltersBelow(t *testing.T) {
	var buf bytes.Buffer
	inner := newBufferHandler(&buf, slog.LevelDebug) // inner accepts all
	filtered := NewLevelFilterHandler(slog.LevelWarn, inner)

	logger := slog.New(filtered)
	logger.Info("should-drop")
	logger.Warn("should-pass")

	if strings.Contains(buf.String(), "should-drop") {
		t.Error("info message should be filtered")
	}
	if !strings.Contains(buf.String(), "should-pass") {
		t.Error("warn message should pass through")
	}
}

func TestLevelFilterHandler_Enabled(t *testing.T) {
	var buf bytes.Buffer
	inner := newBufferHandler(&buf, slog.LevelDebug)
	filtered := NewLevelFilterHandler(slog.LevelWarn, inner)

	if filtered.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("should not be enabled at info")
	}
	if !filtered.Enabled(context.Background(), slog.LevelWarn) {
		t.Error("should be enabled at warn")
	}
}

func TestLevelFilterHandler_WithAttrs(t *testing.T) {
	var buf bytes.Buffer
	inner := newBufferHandler(&buf, slog.LevelDebug)
	filtered := NewLevelFilterHandler(slog.LevelInfo, inner)

	withAttr := filtered.WithAttrs([]slog.Attr{slog.String("k", "v")})
	logger := slog.New(withAttr)
	logger.Info("test")

	if !strings.Contains(buf.String(), "k=v") {
		t.Errorf("missing attr, got: %q", buf.String())
	}
}

func TestLevelFilterHandler_WithAttrsEmpty(t *testing.T) {
	var buf bytes.Buffer
	inner := newBufferHandler(&buf, slog.LevelDebug)
	filtered := NewLevelFilterHandler(slog.LevelInfo, inner)

	same := filtered.WithAttrs(nil)
	if same != filtered {
		t.Error("WithAttrs(nil) should return same handler")
	}
}

func TestLevelFilterHandler_WithGroup(t *testing.T) {
	var buf bytes.Buffer
	inner := newBufferHandler(&buf, slog.LevelDebug)
	filtered := NewLevelFilterHandler(slog.LevelInfo, inner)

	grouped := filtered.WithGroup("grp")
	logger := slog.New(grouped)
	logger.Info("test")

	if !strings.Contains(buf.String(), "grp.test") {
		t.Errorf("missing group prefix, got: %q", buf.String())
	}
}

func TestLevelFilterHandler_WithGroupEmpty(t *testing.T) {
	var buf bytes.Buffer
	inner := newBufferHandler(&buf, slog.LevelDebug)
	filtered := NewLevelFilterHandler(slog.LevelInfo, inner)

	same := filtered.WithGroup("")
	if same != filtered {
		t.Error("WithGroup(\"\") should return same handler")
	}
}

func TestLevelFilterHandler_PanicOnNilInner(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil inner")
		}
	}()
	NewLevelFilterHandler(slog.LevelInfo, nil)
}

func TestLevelFilterHandler_PanicOnNilLevel(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil level")
		}
	}()
	var buf bytes.Buffer
	inner := newBufferHandler(&buf, slog.LevelDebug)
	NewLevelFilterHandler(nil, inner)
}

func TestNewMultiSlogHandler_OTelDisabled(t *testing.T) {
	handler := NewMultiSlogHandler(SlogConfig{
		Level:       slog.LevelInfo,
		Format:      "json",
		OTelEnabled: false,
	})

	// Should not be a MultiHandler — just the stdout handler
	if _, ok := handler.(*MultiHandler); ok {
		t.Error("OTelEnabled=false should not return MultiHandler")
	}
}

func TestNewMultiSlogHandler_OTelEnabled(t *testing.T) {
	handler := NewMultiSlogHandler(SlogConfig{
		ServiceName: "test-svc",
		Level:       slog.LevelInfo,
		Format:      "json",
		OTelEnabled: true,
	})

	multi, ok := handler.(*MultiHandler)
	if !ok {
		t.Fatal("OTelEnabled=true should return MultiHandler")
	}
	if len(multi.handlers) != 2 {
		t.Errorf("expected 2 handlers, got %d", len(multi.handlers))
	}
}

func TestNewMultiSlogHandler_TextFormat(t *testing.T) {
	handler := NewMultiSlogHandler(SlogConfig{
		Level:       slog.LevelInfo,
		Format:      "text",
		OTelEnabled: false,
	})

	if handler == nil {
		t.Fatal("handler should not be nil")
	}
}

// failingHandler always fails and appends an attr to the record it receives,
// to detect handlers sharing one record.
type failingHandler struct{ err error }

func (h failingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h failingHandler) Handle(_ context.Context, r slog.Record) error {
	r.AddAttrs(slog.String("leaked", "from-failing-handler"))
	return h.err
}
func (h failingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h failingHandler) WithGroup(string) slog.Handler      { return h }

// recordingHandler keeps the records it receives and, like real handlers that
// enrich records, adds an attribute to each one.
type recordingHandler struct{ got []slog.Record }

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	r.AddAttrs(slog.String("seen_by", "recorder"))
	h.got = append(h.got, r)
	return nil
}
func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

// AC-9: one failing handler (e.g. stdout) must not stop the record reaching the
// others (e.g. OTel); every failure is returned, and handlers don't share a record.
func TestMultiHandler_FailingHandlerDoesNotBlockOthers(t *testing.T) {
	errA := errors.New("stdout broken")
	errB := errors.New("second failure")
	rec := &recordingHandler{}
	h := NewMultiHandler(failingHandler{err: errA}, rec, failingHandler{err: errB})

	// More attrs than slog.Record stores inline, so copies of the record share
	// backing storage unless each handler gets a Clone.
	r := slog.NewRecord(time.Now(), slog.LevelInfo, "hello", 0)
	for i := 0; i < 10; i++ {
		r.AddAttrs(slog.Int(fmt.Sprintf("a%d", i), i))
	}
	err := h.Handle(context.Background(), r)

	if len(rec.got) != 1 || rec.got[0].Message != "hello" {
		t.Fatalf("second handler did not receive the record: %v", rec.got)
	}
	if !errors.Is(err, errA) || !errors.Is(err, errB) {
		t.Errorf("returned error must contain every failure, got %v", err)
	}
	rec.got[0].Attrs(func(a slog.Attr) bool {
		// slog adds a "!BUG" attr when AddAttrs is called on a shared copy.
		if a.Key == "leaked" || strings.HasPrefix(a.Key, "!BUG") {
			t.Errorf("record shared between handlers: saw %v", a)
		}
		return true
	})
}
