package go_logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
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
}

// DefaultConfig возвращает стандартную конфигурацию
func DefaultConfig() Config {
	return Config{
		Level:     slog.LevelInfo,
		JSON:      true,
		AddSource: false,
		Output:    os.Stderr,
		Service:   "unknown",
		Env:       "unknown",
		Version:   "dev",
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

	if cfg.JSON {
		return slog.NewJSONHandler(cfg.Output, opts)
	}
	return slog.NewTextHandler(cfg.Output, opts)
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

// Close — сбрасывает буфер (если Output — *os.File)
func (l *Logger) Close() error {
	if f, ok := l.Config.Output.(*os.File); ok {
		return f.Sync()
	}
	return nil
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
