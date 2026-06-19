// Package logging configures structured application logging.
package logging

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
)

const (
	// DefaultLevel disables logging unless the user explicitly enables it.
	DefaultLevel = "off"
	// DefaultTarget sends logs to stderr by default so stdout stays machine-readable.
	DefaultTarget = "stderr"
	// DefaultFile is used when --log-target=file is selected without an explicit path.
	DefaultFile = "./debug.log"
)

// Config is the normalized logging configuration parsed from CLI flags and environment variables.
type Config struct {
	Level  string
	Target string
	File   string
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

// New creates a slog logger and a flush/cleanup function. When bufferTerminal is true,
// stdout/stderr targets are buffered until Flush is called so TUI rendering is not corrupted.
func New(cfg Config, bufferTerminal bool) (*slog.Logger, func() error, error) {
	level, enabled, err := parseLevel(cfg.Level)
	if err != nil {
		return nil, nil, err
	}
	if !enabled {
		return discardLogger, func() error { return nil }, nil
	}

	target := strings.ToLower(normalizeDefault(cfg.Target, DefaultTarget))
	file := normalizeDefault(cfg.File, DefaultFile)

	var writer io.Writer
	flush := func() error { return nil }
	switch target {
	case "stderr":
		if bufferTerminal {
			buf := &lockedBuffer{}
			writer = buf
			flush = func() error { return buf.FlushTo(os.Stderr) }
		} else {
			writer = os.Stderr
		}
	case "stdout":
		if bufferTerminal {
			buf := &lockedBuffer{}
			writer = buf
			flush = func() error { return buf.FlushTo(os.Stdout) }
		} else {
			writer = os.Stdout
		}
	case "file":
		f, err := os.OpenFile(file, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, nil, fmt.Errorf("open log file %s: %w", file, err)
		}
		writer = f
		flush = f.Close
	default:
		return nil, nil, fmt.Errorf("unsupported --log-target %q; expected stderr, stdout, or file", cfg.Target)
	}

	logger := slog.New(slog.NewTextHandler(writer, &slog.HandlerOptions{Level: level}))
	return logger, flush, nil
}

func parseLevel(value string) (slog.Level, bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", DefaultLevel:
		return slog.LevelInfo, false, nil
	case "debug":
		return slog.LevelDebug, true, nil
	case "info":
		return slog.LevelInfo, true, nil
	case "warn", "warning":
		return slog.LevelWarn, true, nil
	case "error":
		return slog.LevelError, true, nil
	default:
		return slog.LevelInfo, false, errors.New("unsupported --log-level " + strconvQuote(value) + "; expected debug, info, warn, error, or off")
	}
}

func normalizeDefault(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func strconvQuote(value string) string {
	return fmt.Sprintf("%q", value)
}
