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

// Logger — обёртка над *slog.Logger
type Logger struct {
	slogger *slog.Logger
	Config  Config
	attrs   []any
	groups  []string
}

// Config — настройки логгера
type Config struct {
	Level     slog.Level
	JSON      bool
	AddSource bool
	Output    io.Writer
	Service   string
	Env       string
	Version   string

	// Форматирование времени
	TimeFormat string         // "2006-01-02 15:04:05", "RFC3339", "" (default)
	Timezone   *time.Location // time.UTC, time.Local, или custom
	TimeKey    string         // ключ для времени в JSON (default: "time")

	// Цветной вывод для консоли
	ColorOutput bool // включить цвета для текстового вывода

	// Асинхронное логирование
	Async           bool // включить асинхронное логирование
	AsyncBufferSize int  // размер буфера для асинхронного логирования
}

// Color — ANSI коды цветов
type Color struct {
	Code string
	Bold bool
}

// Цвета для уровней логирования
var (
	ColorDebug = Color{Code: "\x1b[36m", Bold: false} // Cyan
	ColorInfo  = Color{Code: "\x1b[32m", Bold: false} // Green
	ColorWarn  = Color{Code: "\x1b[33m", Bold: false} // Yellow
	ColorError = Color{Code: "\x1b[31m", Bold: true}  // Red bold
	ColorReset = "\x1b[0m"
)

// LevelColor возвращает цвет для уровня логирования
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

// DefaultConfig возвращает стандартную конфигурацию
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

// Init инициализирует логгер. Можно вызывать только один раз.
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

// newHandler создаёт обработчик
func newHandler(cfg Config) slog.Handler {
	opts := &slog.HandlerOptions{
		Level:     cfg.Level,
		AddSource: cfg.AddSource,
	}

	var output io.Writer = cfg.Output

	// Асинхронное логирование
	if cfg.Async {
		output = newAsyncWriter(cfg.Output, cfg.AsyncBufferSize)
	}

	var baseHandler slog.Handler
	if cfg.JSON {
		baseHandler = slog.NewJSONHandler(output, opts)
	} else {
		baseHandler = slog.NewTextHandler(output, opts)
	}

	// Оборачиваем в customHandler только если нужна кастомизация
	// (цвета или кастомный формат времени)
	needsCustomHandler := cfg.ColorOutput && !cfg.JSON || cfg.TimeFormat != ""

	if !needsCustomHandler {
		return baseHandler
	}

	// Оборачиваем в customHandler для форматирования времени и цветов
	return &customHandler{
		handler:     baseHandler,
		output:      output,
		timeFormat:  cfg.TimeFormat,
		timezone:    cfg.Timezone,
		timeKey:     cfg.TimeKey,
		colorOutput: cfg.ColorOutput && !cfg.JSON,
	}
}

// customHandler — кастомный обработчик для форматирования времени и цветов
type customHandler struct {
	handler     slog.Handler
	output      io.Writer
	timeFormat  string
	timezone    *time.Location
	timeKey     string
	colorOutput bool
	attrs       []slog.Attr // сохранённые атрибуты из WithAttrs
	mu          sync.Mutex
}

func (h *customHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

func (h *customHandler) Handle(ctx context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Для текстового вывода с цветами или кастомным форматом используем свой формат
	if h.colorOutput || h.timeFormat != "" {
		// Форматируем время
		if h.timeFormat != "" {
			tz := h.timezone
			if tz == nil {
				tz = time.Local
			}
			r.Time = r.Time.In(tz)
		}
		return h.handleWithFormat(ctx, r)
	}

	// Иначе используем стандартный handler
	return h.handler.Handle(ctx, r)
}

func (h *customHandler) handleWithFormat(ctx context.Context, r slog.Record) error {
	var buf bytes.Buffer

	// Время
	timeFormat := h.timeFormat
	if timeFormat == "" {
		timeFormat = "2006-01-02 15:04:05" // формат по умолчанию
	}
	buf.WriteString(r.Time.Format(timeFormat))
	buf.WriteString(" ")

	// Уровень с цветом (если включено)
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

	// Сообщение
	buf.WriteString(r.Message)

	// Сначала выводим сохранённые атрибуты из WithAttrs
	for _, attr := range h.attrs {
		buf.WriteString(" ")
		buf.WriteString(attr.Key)
		buf.WriteString("=")
		buf.WriteString(attr.Value.String())
	}

	// Затем атрибуты из текущей записи
	r.Attrs(func(a slog.Attr) bool {
		buf.WriteString(" ")
		buf.WriteString(a.Key)
		buf.WriteString("=")
		buf.WriteString(a.Value.String())
		return true
	})

	buf.WriteString("\n")

	// Записываем в output
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
		attrs:       append(h.attrs, attrs...), // сохраняем атрибуты
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

// asyncWriter — асинхронный writer для логирования
type asyncWriter struct {
	writer io.Writer
	ch     chan []byte
	wg     sync.WaitGroup
	once   sync.Once
}

func newAsyncWriter(w io.Writer, bufferSize int) *asyncWriter {
	aw := &asyncWriter{
		writer: w,
		ch:     make(chan []byte, bufferSize),
	}
	aw.wg.Add(1)
	go aw.process()
	return aw
}

func (aw *asyncWriter) process() {
	defer aw.wg.Done()
	for data := range aw.ch {
		aw.writer.Write(data)
	}
}

func (aw *asyncWriter) Write(p []byte) (int, error) {
	// Копируем данные, чтобы избежать гонки данных
	data := make([]byte, len(p))
	copy(data, p)

	select {
	case aw.ch <- data:
		return len(p), nil
	default:
		// Буфер полон, пишем синхронно
		return aw.writer.Write(p)
	}
}

func (aw *asyncWriter) Close() error {
	aw.once.Do(func() {
		close(aw.ch)
		aw.wg.Wait()
	})
	return nil
}

// Get возвращает инициализированный логгер
func Get() *Logger {
	if instance == nil {
		panic("logging: Init must be called before Get")
	}
	return instance
}

// Slog возвращает *slog.Logger для совместимости
func (l *Logger) Slog() *slog.Logger {
	return l.slogger
}

// Логирование с уровнем
func (l *Logger) Debug(msg string, args ...any) {
	l.slogger.Debug(msg, args...)
}

func (l *Logger) Info(msg string, args ...any) {
	l.slogger.Info(msg, args...)
}

func (l *Logger) Warn(msg string, args ...any) {
	l.slogger.Warn(msg, args...)
}

func (l *Logger) Error(msg string, args ...any) {
	l.slogger.Error(msg, args...)
}

// Fatal — логирует и завершает программу
func (l *Logger) Fatal(msg string, args ...any) {
	l.slogger.Error(msg, args...)
	os.Exit(1)
}

// Log — с контекстом и уровнем
func (l *Logger) Log(ctx context.Context, level slog.Level, msg string, args ...any) {
	l.slogger.Log(ctx, level, msg, args...)
}

func (l *Logger) With(args ...any) *Logger {
	return &Logger{
		slogger: l.slogger.With(args...),
		Config:  l.Config,
		attrs:   append(l.attrs, args...), // Сохраняем
		groups:  l.groups,
	}
}

func (l *Logger) WithGroup(name string) *Logger {
	return &Logger{
		slogger: l.slogger.WithGroup(name),
		Config:  l.Config,
		attrs:   l.attrs,
		groups:  append(l.groups, name), // Сохраняем
	}
}

// SetLevel — изменяет уровень логирования
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

// Close — сбрасывает буфер и закрывает асинхронный writer
func (l *Logger) Close() error {
	// Закрываем asyncWriter если используется
	if l.Config.Async {
		if aw, ok := l.Config.Output.(*asyncWriter); ok {
			aw.Close()
		}
	}

	// Сбрасываем буфер файла
	if f, ok := l.Config.Output.(*os.File); ok {
		return f.Sync()
	}
	return nil
}

// WithRequestID добавляет request ID к логгеру
func (l *Logger) WithRequestID(id string) *Logger {
	return l.With(slog.String("request_id", id))
}

// WithTraceID добавляет trace ID к логгеру
func (l *Logger) WithTraceID(id string) *Logger {
	return l.With(slog.String("trace_id", id))
}

// WithUserID добавляет user ID к логгеру
func (l *Logger) WithUserID(id string) *Logger {
	return l.With(slog.String("user_id", id))
}

// WithField добавляет произвольное поле к логгеру
func (l *Logger) WithField(key string, value any) *Logger {
	return l.With(slog.Any(key, value))
}

// WithFields добавляет несколько полей к логгеру
func (l *Logger) WithFields(fields map[string]any) *Logger {
	args := make([]any, 0, len(fields)*2)
	for k, v := range fields {
		args = append(args, k, v)
	}
	return l.With(args...)
}

// ParseLevel — парсит строку в slog.Level
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
