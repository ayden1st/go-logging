// Package go_logging provides a structured logging wrapper around Go's slog package.
// It supports JSON and text output, colored console output, custom time formatting,
// timezone support, and asynchronous logging.
package go_logging

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

var (
	instance *Logger
	once     sync.Once
)

// Logger is a wrapper around *slog.Logger that provides additional functionality
// such as request/trace/user ID helpers, field management, and configuration.
type Logger struct {
	slogger *slog.Logger
	Config  Config
	attrs   []any
	groups  []string
}

// Config holds the configuration settings for the logger.
type Config struct {
	Level     slog.Level
	JSON      bool
	AddSource bool
	Output    io.Writer
	Service   string
	Env       string
	Version   string

	// Time formatting options
	TimeFormat string         // "2006-01-02 15:04:05", "RFC3339", "" (default)
	Timezone   *time.Location // time.UTC, time.Local, or custom
	TimeKey    string         // key for time in JSON (default: "time")

	// Color output for console
	ColorOutput bool // enable colors for text output

	// Asynchronous logging
	Async           bool // enable asynchronous logging
	AsyncBufferSize int  // buffer size for asynchronous logging
}

// Color represents ANSI color codes with optional bold formatting.
type Color struct {
	Code string
	Bold bool
}

// Color constants for different log levels.
var (
	ColorDebug = Color{Code: "\x1b[36m", Bold: false} // Cyan
	ColorInfo  = Color{Code: "\x1b[32m", Bold: false} // Green
	ColorWarn  = Color{Code: "\x1b[33m", Bold: false} // Yellow
	ColorError = Color{Code: "\x1b[31m", Bold: true}  // Red bold
	ColorReset = "\x1b[0m"
)

// LevelColor returns the appropriate color for a given log level.
func LevelColor(level slog.Level) Color {
	switch {
	case level < slog.LevelInfo:
		return ColorDebug
	case level < slog.LevelWarn:
		return ColorInfo
	case level < slog.LevelError:
		return ColorWarn
	default:
		return ColorError
	}
}

// DefaultConfig returns a configuration with sensible defaults.
// Defaults: Level=Info, JSON=true, AddSource=false, Output=stderr,
// TimeFormat="2006-01-02 15:04:05", Timezone=Local, ColorOutput=false, Async=false.
func DefaultConfig() Config {
	return Config{
		Level:           slog.LevelInfo,
		JSON:            true,
		AddSource:       false,
		Output:          os.Stderr,
		Service:         "unknown",
		Env:             "unknown",
		Version:         "dev",
		TimeFormat:      "2006-01-02 15:04:05",
		Timezone:        time.Local,
		TimeKey:         "time",
		ColorOutput:     false,
		Async:           false,
		AsyncBufferSize: 1000,
	}
}

// Init initializes the global logger instance. This function can only be called once.
// Subsequent calls will return the already initialized instance.
// The logger is automatically configured with service, env, and version attributes
// if they are provided in the config.
func Init(cfg Config) *Logger {
	once.Do(func() {
		handler := newHandler(cfg)
		logger := slog.New(handler)

		if cfg.Service != "" || cfg.Env != "" || cfg.Version != "" {
			logger = logger.With(
				slog.String("service", cfg.Service),
				slog.String("env", cfg.Env),
				slog.String("version", cfg.Version),
			)
		}

		instance = &Logger{
			slogger: logger,
			Config:  cfg,
		}
	})
	return instance
}

// newHandler creates a new slog.Handler based on the provided configuration.
// It supports JSON/text output, async logging, custom time formatting, and colored output.
func newHandler(cfg Config) slog.Handler {
	opts := &slog.HandlerOptions{
		Level:     cfg.Level,
		AddSource: cfg.AddSource,
	}

	var output io.Writer = cfg.Output

	if cfg.Async {
		output = newAsyncWriter(cfg.Output, cfg.AsyncBufferSize)
	}

	var baseHandler slog.Handler
	if cfg.JSON {
		baseHandler = slog.NewJSONHandler(output, opts)
	} else {
		baseHandler = slog.NewTextHandler(output, opts)
	}

	needsCustomHandler := cfg.ColorOutput && !cfg.JSON || cfg.TimeFormat != ""

	if !needsCustomHandler {
		return baseHandler
	}

	return &customHandler{
		handler:     baseHandler,
		output:      output,
		timeFormat:  cfg.TimeFormat,
		timezone:    cfg.Timezone,
		timeKey:     cfg.TimeKey,
		colorOutput: cfg.ColorOutput && !cfg.JSON,
	}
}

// customHandler is a custom slog.Handler that provides custom time formatting
// and colored console output.
type customHandler struct {
	handler     slog.Handler
	output      io.Writer
	timeFormat  string
	timezone    *time.Location
	timeKey     string
	colorOutput bool
	attrs       []slog.Attr
	mu          sync.Mutex
}

func (h *customHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

func (h *customHandler) Handle(ctx context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.colorOutput || h.timeFormat != "" {
		if h.timeFormat != "" {
			tz := h.timezone
			if tz == nil {
				tz = time.Local
			}
			r.Time = r.Time.In(tz)
		}
		return h.handleWithFormat(ctx, r)
	}

	return h.handler.Handle(ctx, r)
}

func (h *customHandler) handleWithFormat(ctx context.Context, r slog.Record) error {
	var buf bytes.Buffer

	timeFormat := h.timeFormat
	if timeFormat == "" {
		timeFormat = "2006-01-02 15:04:05" // формат по умолчанию
	}
	buf.WriteString(r.Time.Format(timeFormat))
	buf.WriteString(" ")

	if h.colorOutput {
		color := LevelColor(r.Level)
		if color.Bold {
			buf.WriteString("\x1b[1m")
		}
		buf.WriteString(color.Code)
		buf.WriteString(r.Level.String())
		buf.WriteString(ColorReset)
		if color.Bold {
			buf.WriteString("\x1b[22m")
		}
		buf.WriteString(" ")
	} else {
		buf.WriteString(r.Level.String())
		buf.WriteString(" ")
	}

	buf.WriteString(r.Message)

	for _, attr := range h.attrs {
		buf.WriteString(" ")
		buf.WriteString(attr.Key)
		buf.WriteString("=")
		buf.WriteString(attr.Value.String())
	}

	r.Attrs(func(a slog.Attr) bool {
		buf.WriteString(" ")
		buf.WriteString(a.Key)
		buf.WriteString("=")
		buf.WriteString(a.Value.String())
		return true
	})

	buf.WriteString("\n")

	_, err := h.output.Write(buf.Bytes())
	return err
}

func (h *customHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &customHandler{
		handler:     h.handler.WithAttrs(attrs),
		output:      h.output,
		timeFormat:  h.timeFormat,
		timezone:    h.timezone,
		timeKey:     h.timeKey,
		colorOutput: h.colorOutput,
		attrs:       append(h.attrs, attrs...),
	}
}

func (h *customHandler) WithGroup(name string) slog.Handler {
	return &customHandler{
		handler:     h.handler.WithGroup(name),
		output:      h.output,
		timeFormat:  h.timeFormat,
		timezone:    h.timezone,
		timeKey:     h.timeKey,
		colorOutput: h.colorOutput,
	}
}

// asyncWriter is an asynchronous writer that buffers log messages and writes them
// in a separate goroutine. If the buffer is full, it falls back to synchronous writing.
type asyncWriter struct {
	writer io.Writer
	ch     chan []byte
	wg     sync.WaitGroup
	once   sync.Once
}

// newAsyncWriter creates a new asyncWriter with the specified buffer size.
// The writer starts a background goroutine to process log messages.
func newAsyncWriter(w io.Writer, bufferSize int) *asyncWriter {
	aw := &asyncWriter{
		writer: w,
		ch:     make(chan []byte, bufferSize),
	}
	aw.wg.Add(1)
	go aw.process()
	return aw
}

// process runs in a background goroutine and writes buffered messages to the underlying writer.
func (aw *asyncWriter) process() {
	defer aw.wg.Done()
	for data := range aw.ch {
		aw.writer.Write(data)
	}
}

// Write writes data to the async buffer. If the buffer is full, it falls back to
// synchronous writing to avoid blocking.
func (aw *asyncWriter) Write(p []byte) (int, error) {
	// Copy data to avoid data race
	data := make([]byte, len(p))
	copy(data, p)

	select {
	case aw.ch <- data:
		return len(p), nil
	default:
		// Buffer is full, write synchronously
		return aw.writer.Write(p)
	}
}

// Close closes the async channel and waits for all buffered messages to be written.
func (aw *asyncWriter) Close() error {
	aw.once.Do(func() {
		close(aw.ch)
		aw.wg.Wait()
	})
	return nil
}

// Get returns the initialized global logger instance.
// Panics if Init has not been called.
func Get() *Logger {
	if instance == nil {
		panic("logging: Init must be called before Get")
	}
	return instance
}

// Slog returns the underlying *slog.Logger for compatibility with slog-based code.
func (l *Logger) Slog() *slog.Logger {
	return l.slogger
}

// Debug logs a message at Debug level.
func (l *Logger) Debug(msg string, args ...any) {
	l.slogger.Debug(msg, args...)
}

// Info logs a message at Info level.
func (l *Logger) Info(msg string, args ...any) {
	l.slogger.Info(msg, args...)
}

// Warn logs a message at Warn level.
func (l *Logger) Warn(msg string, args ...any) {
	l.slogger.Warn(msg, args...)
}

// Error logs a message at Error level.
func (l *Logger) Error(msg string, args ...any) {
	l.slogger.Error(msg, args...)
}

// Fatal logs a message at Error level and terminates the program with exit code 1.
func (l *Logger) Fatal(msg string, args ...any) {
	l.slogger.Error(msg, args...)
	os.Exit(1)
}

// Log logs a message at the specified level with the given context.
func (l *Logger) Log(ctx context.Context, level slog.Level, msg string, args ...any) {
	l.slogger.Log(ctx, level, msg, args...)
}

// With returns a new Logger with the given key-value pairs added to all log entries.
func (l *Logger) With(args ...any) *Logger {
	return &Logger{
		slogger: l.slogger.With(args...),
		Config:  l.Config,
		attrs:   append(l.attrs, args...), // Сохраняем
		groups:  l.groups,
	}
}

// WithGroup returns a new Logger with a group name. All subsequent attributes
// will be grouped under this name.
func (l *Logger) WithGroup(name string) *Logger {
	return &Logger{
		slogger: l.slogger.WithGroup(name),
		Config:  l.Config,
		attrs:   l.attrs,
		groups:  append(l.groups, name), // Сохраняем
	}
}

// SetLevel changes the log level dynamically. Preserves existing groups and attributes.
func (l *Logger) SetLevel(level slog.Level) {
	l.Config.Level = level
	handler := newHandler(l.Config)
	l.slogger = slog.New(handler)

	// Восстанавливаем группы и атрибуты
	for _, group := range l.groups {
		l.slogger = l.slogger.WithGroup(group)
	}
	if len(l.attrs) > 0 {
		l.slogger = l.slogger.With(l.attrs...)
	}
}

// Close flushes any buffered logs and closes the async writer if enabled.
// Also syncs the file buffer if writing to a file.
func (l *Logger) Close() error {
	// Close asyncWriter if in use
	if l.Config.Async {
		if aw, ok := l.Config.Output.(*asyncWriter); ok {
			aw.Close()
		}
	}

	// Flush file buffer
	if f, ok := l.Config.Output.(*os.File); ok {
		return f.Sync()
	}
	return nil
}

// WithRequestID returns a new Logger with a request_id field added to all log entries.
func (l *Logger) WithRequestID(id string) *Logger {
	return l.With(slog.String("request_id", id))
}

// WithTraceID returns a new Logger with a trace_id field added to all log entries.
func (l *Logger) WithTraceID(id string) *Logger {
	return l.With(slog.String("trace_id", id))
}

// WithUserID returns a new Logger with a user_id field added to all log entries.
func (l *Logger) WithUserID(id string) *Logger {
	return l.With(slog.String("user_id", id))
}

// WithField returns a new Logger with a custom field added to all log entries.
func (l *Logger) WithField(key string, value any) *Logger {
	return l.With(slog.Any(key, value))
}

// WithFields returns a new Logger with multiple fields added to all log entries.
func (l *Logger) WithFields(fields map[string]any) *Logger {
	args := make([]any, 0, len(fields)*2)
	for k, v := range fields {
		args = append(args, k, v)
	}
	return l.With(args...)
}

// ParseLevel converts a string to a slog.Level.
// Supported values (case-insensitive): "debug", "info", "warn", "warning", "error".
// Defaults to Info level for unrecognized values.
func ParseLevel(levelStr string) slog.Level {
	switch strings.ToLower(levelStr) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
