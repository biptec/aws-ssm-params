// Package logging configures structured application logging.
package logging

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/cockroachdb/errors"
)

const (
	// DefaultLevel disables logging unless the user explicitly enables it.
	DefaultLevel = "off"
	// LevelTrace is more verbose than slog.LevelDebug and is reserved for low-level diagnostics.
	LevelTrace slog.Level = slog.LevelDebug - 4
)

// Config is the normalized logging configuration parsed from CLI flags and environment variables.
type Config struct {
	Level string
}

type contextKey struct{}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	n, err := b.buf.Write(p)
	if err != nil {
		return n, fmt.Errorf("write buffered log: %w", err)
	}

	return n, nil
}

func (b *lockedBuffer) FlushTo(w io.Writer) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.buf.Len() == 0 {
		return nil
	}

	if _, err := w.Write(b.buf.Bytes()); err != nil {
		return fmt.Errorf("flush buffered logs: %w", err)
	}

	b.buf.Reset()

	return nil
}

var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

// WithLogger attaches a configured logger to a context.
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}

	if logger == nil {
		logger = discardLogger
	}

	return context.WithValue(ctx, contextKey{}, logger)
}

// FromContext returns the logger attached to a context or a disabled logger.
func FromContext(ctx context.Context) *slog.Logger {
	if ctx == nil {
		return discardLogger
	}

	logger, ok := ctx.Value(contextKey{}).(*slog.Logger)
	if !ok || logger == nil {
		return discardLogger
	}

	return logger
}

// TraceEnabled reports whether the context logger accepts trace-level records.
func TraceEnabled(ctx context.Context) bool {
	return FromContext(ctx).Enabled(ctx, LevelTrace)
}

// New creates a slog logger and a flush/cleanup function.
// Logging is disabled by default. Any enabled level writes to stderr; users can redirect stderr
// to a file with standard shell redirection. During TUI sessions, logs are buffered only when
// stderr is an interactive terminal. Redirected stderr receives logs immediately.
func New(cfg Config, bufferTerminal bool) (*slog.Logger, func() error, error) {
	level, enabled, err := parseLevel(cfg.Level)
	if err != nil {
		return nil, nil, err
	}

	if !enabled {
		return discardLogger, func() error { return nil }, nil
	}

	writer := io.Writer(os.Stderr)
	flush := func() error { return nil }

	if bufferTerminal && isTerminalFile(os.Stderr) {
		buf := &lockedBuffer{}
		writer = buf
		flush = func() error { return buf.FlushTo(os.Stderr) }
	}

	logger := slog.New(slog.NewTextHandler(writer, &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: replaceLevelAttr,
	}))

	return logger, flush, nil
}

func replaceLevelAttr(groups []string, attr slog.Attr) slog.Attr {
	_ = groups

	if attr.Key != slog.LevelKey {
		return attr
	}

	level, ok := attr.Value.Any().(slog.Level)
	if !ok {
		return attr
	}

	if level == LevelTrace {
		return slog.String(slog.LevelKey, "TRACE")
	}

	return attr
}

func parseLevel(value string) (slog.Level, bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", DefaultLevel:
		return slog.LevelInfo, false, nil
	case "trace":
		return LevelTrace, true, nil
	case "debug":
		return slog.LevelDebug, true, nil
	case "info":
		return slog.LevelInfo, true, nil
	case "warn", "warning":
		return slog.LevelWarn, true, nil
	case "error":
		return slog.LevelError, true, nil
	default:
		return slog.LevelInfo, false, errors.New("unsupported --log-level " + strconvQuote(value) + "; expected trace, debug, info, warn, error, or off")
	}
}

func isTerminalFile(file *os.File) bool {
	if file == nil {
		return false
	}

	info, err := file.Stat()
	if err != nil {
		return false
	}

	return info.Mode()&os.ModeCharDevice != 0
}

func strconvQuote(value string) string {
	return fmt.Sprintf("%q", value)
}
