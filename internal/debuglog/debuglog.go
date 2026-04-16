// Package debuglog owns the optional structured log file at
// $XDG_CACHE_HOME/scout/debug.log (or $HOME/.cache/scout/debug.log).
// It is gated on the environment variable SCOUT_DEBUG=1. When the
// variable is unset or set to any other value, all exported functions
// return no-op implementations so the rest of the program can call them
// unconditionally.
//
// The file is TRUNCATED at the start of each program run so a single
// log file represents a single session — easier to share when
// reproducing a bug. Log rotation is not implemented.
package debuglog

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/aws/smithy-go/logging"
)

// envVar is the gate that decides whether debug logging is active.
const envVar = "SCOUT_DEBUG"

// enabled caches the result of the env-var check at Init time so later
// callers don't have to re-consult the environment.
var (
	enabled   bool
	handle    *os.File
	logger    *slog.Logger
	sdkLogger logging.Logger = logging.Nop{}
)

// Init wires up the debug log. It returns a close function that the
// caller should defer from main; the close function is always safe to
// call, even when logging is disabled.
//
// If SCOUT_DEBUG is unset, Init is a no-op and the returned close
// function does nothing. Any error opening the log file is reported
// on stderr (because the TUI hasn't started yet) and the function
// degrades gracefully to a no-op logger.
func Init() func() {
	if os.Getenv(envVar) != "1" {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
		return func() {}
	}

	path, err := logPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "scout: cannot resolve debug log path: %v\n", err)
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
		return func() {}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "scout: cannot create debug log dir: %v\n", err)
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
		return func() {}
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "scout: cannot open debug log %s: %v\n", path, err)
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
		return func() {}
	}

	enabled = true
	handle = f
	logger = slog.New(slog.NewJSONHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug}))
	sdkLogger = smithyAdapter{logger: logger}

	logger.Info("debug log started", "path", path)

	return func() {
		if handle != nil {
			_ = handle.Sync()
			_ = handle.Close()
			handle = nil
		}
	}
}

// Enabled reports whether the debug log is active for this run.
func Enabled() bool { return enabled }

// Logger returns the app-level slog.Logger. When disabled, it returns
// a logger that drops every record.
func Logger() *slog.Logger {
	if logger == nil {
		return slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	return logger
}

// SDKLogger returns a smithy-go logging.Logger suitable for plugging
// into aws.Config.Logger. When disabled, the returned logger is
// smithy's Nop{}.
func SDKLogger() logging.Logger { return sdkLogger }

// logPath resolves the absolute location of the debug log file.
func logPath() (string, error) {
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "scout", "debug.log"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "scout", "debug.log"), nil
}

// smithyAdapter routes aws-sdk-go-v2 log records (delivered via the
// smithy logging.Logger interface) into our slog.Logger. The adapter
// is used only when debug logging is enabled; otherwise awsctx wires
// logging.Nop{} directly.
type smithyAdapter struct {
	logger *slog.Logger
}

func (a smithyAdapter) Logf(classification logging.Classification, format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	switch classification {
	case logging.Warn:
		a.logger.LogAttrs(context.Background(), slog.LevelWarn, msg, slog.String("source", "sdk"))
	case logging.Debug:
		a.logger.LogAttrs(context.Background(), slog.LevelDebug, msg, slog.String("source", "sdk"))
	default:
		a.logger.LogAttrs(context.Background(), slog.LevelInfo, msg, slog.String("source", "sdk"))
	}
}
