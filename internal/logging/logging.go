package logging

import (
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

// LoggingConfig mirrors the config.LoggingConfig to avoid import cycles
type LoggingConfig struct {
	Level     string
	Directory string
}

// Setup configures the default slog logger based on the provided configuration.
// It accepts a LoggingConfig struct.
func Setup(cfg LoggingConfig) {
	level := parseSlogLevel(cfg.Level)
	opts := &slog.HandlerOptions{
		Level: level,
	}
	handler := slog.NewTextHandler(os.Stdout, opts)
	slog.SetDefault(slog.New(handler))
}

// SetupWithLevel configures the default slog logger with the specified level string.
func SetupWithLevel(levelStr string) {
	level := parseSlogLevel(levelStr)
	opts := &slog.HandlerOptions{
		Level: level,
	}
	handler := slog.NewTextHandler(os.Stdout, opts)
	slog.SetDefault(slog.New(handler))
}

// parseSlogLevel converts a string level to slog.Level
func parseSlogLevel(s string) slog.Level {
	switch strings.ToLower(s) {
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

// Level represents log levels
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

// String returns the string representation of a log level
func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// ParseLevel parses a level string to Level
func ParseLevel(s string) Level {
	switch strings.ToLower(s) {
	case "debug":
		return LevelDebug
	case "info":
		return LevelInfo
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

// Logger provides structured logging with level filtering and field support
type Logger struct {
	level     Level
	output    io.Writer
	mu        sync.Mutex
	fields    map[string]interface{}
	sensitive []string // field names to redact
}

// Config holds logger configuration
type Config struct {
	Level     string
	Output    io.Writer
	Sensitive []string // field names to redact (e.g., "token", "api_key")
}

// New creates a new logger with default configuration
func New() *Logger {
	return NewWithConfig(Config{
		Level:     "info",
		Output:    os.Stdout,
		Sensitive: []string{"token", "api_key", "password", "secret", "authorization"},
	})
}

// NewWithConfig creates a new logger with the specified configuration
func NewWithConfig(cfg Config) *Logger {
	output := cfg.Output
	if output == nil {
		output = os.Stdout
	}
	return &Logger{
		level:     ParseLevel(cfg.Level),
		output:    output,
		fields:    make(map[string]interface{}),
		sensitive: cfg.Sensitive,
	}
}

// WithFields returns a new logger with additional fields
func (l *Logger) WithFields(fields map[string]interface{}) *Logger {
	newLogger := &Logger{
		level:     l.level,
		output:    l.output,
		fields:    make(map[string]interface{}),
		sensitive: l.sensitive,
	}
	// Copy existing fields
	for k, v := range l.fields {
		newLogger.fields[k] = v
	}
	// Add new fields
	for k, v := range fields {
		newLogger.fields[k] = v
	}
	return newLogger
}

// WithField returns a new logger with an additional field
func (l *Logger) WithField(key string, value interface{}) *Logger {
	return l.WithFields(map[string]interface{}{key: value})
}

// SetLevel sets the log level
func (l *Logger) SetLevel(level Level) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// log writes a log entry
func (l *Logger) log(level Level, msg string, fields ...interface{}) {
	if level < l.level {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	timestamp := time.Now().Format("2006-01-02 15:04:05.000")

	// Build field string
	var fieldStr strings.Builder

	// Add stored fields first
	for k, v := range l.fields {
		fieldStr.WriteString(fmt.Sprintf(" %s=%v", k, l.redact(k, v)))
	}

	// Add inline fields (key=value pairs)
	for i := 0; i+1 < len(fields); i += 2 {
		key, ok := fields[i].(string)
		if !ok {
			continue
		}
		value := fields[i+1]
		fieldStr.WriteString(fmt.Sprintf(" %s=%v", key, l.redact(key, value)))
	}

	logLine := fmt.Sprintf("%s [%s] %s%s\n", timestamp, level.String(), msg, fieldStr.String())
	l.output.Write([]byte(logLine))
}

// redact redacts sensitive field values
func (l *Logger) redact(key string, value interface{}) interface{} {
	keyLower := strings.ToLower(key)
	for _, sensitive := range l.sensitive {
		if strings.Contains(keyLower, strings.ToLower(sensitive)) {
			return "[REDACTED]"
		}
	}
	return value
}

// Debug logs a debug message
func (l *Logger) Debug(msg string, fields ...interface{}) {
	l.log(LevelDebug, msg, fields...)
}

// Info logs an info message
func (l *Logger) Info(msg string, fields ...interface{}) {
	l.log(LevelInfo, msg, fields...)
}

// Warn logs a warning message
func (l *Logger) Warn(msg string, fields ...interface{}) {
	l.log(LevelWarn, msg, fields...)
}

// Error logs an error message
func (l *Logger) Error(msg string, fields ...interface{}) {
	l.log(LevelError, msg, fields...)
}

// Fatal logs an error message and exits
func (l *Logger) Fatal(msg string, fields ...interface{}) {
	l.log(LevelError, msg, fields...)
	os.Exit(1)
}

// Default logger instance
var defaultLogger = New()

// SetDefault sets the default logger
func SetDefault(logger *Logger) {
	defaultLogger = logger
}

// Default returns the default logger
func Default() *Logger {
	return defaultLogger
}

// Package-level convenience functions using default logger

// Debug logs a debug message using the default logger
func Debug(msg string, fields ...interface{}) {
	defaultLogger.Debug(msg, fields...)
}

// Info logs an info message using the default logger
func Info(msg string, fields ...interface{}) {
	defaultLogger.Info(msg, fields...)
}

// Warn logs a warning message using the default logger
func Warn(msg string, fields ...interface{}) {
	defaultLogger.Warn(msg, fields...)
}

// Error logs an error message using the default logger
func Error(msg string, fields ...interface{}) {
	defaultLogger.Error(msg, fields...)
}

// StdLogger returns a standard library *log.Logger that writes to this logger
func (l *Logger) StdLogger(level Level) *log.Logger {
	return log.New(&logAdapter{logger: l, level: level}, "", 0)
}

type logAdapter struct {
	logger *Logger
	level  Level
}

func (a *logAdapter) Write(p []byte) (n int, err error) {
	msg := strings.TrimSpace(string(p))
	a.logger.log(a.level, msg)
	return len(p), nil
}
