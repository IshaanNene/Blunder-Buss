package logging

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"
)

// Logger interface for structured logging
type Logger interface {
	WithCorrelationID(id string) Logger
	WithFields(fields map[string]interface{}) Logger
	WithField(key string, value interface{}) Logger
	Info(msg string)
	Error(msg string, err error)
	Warn(msg string)
	Debug(msg string)
}

// LogEntry represents a structured log entry
type LogEntry struct {
	Timestamp     time.Time              `json:"timestamp"`
	Level         string                 `json:"level"`
	Message       string                 `json:"message"`
	CorrelationID string                 `json:"correlation_id,omitempty"`
	Service       string                 `json:"service"`
	Fields        map[string]interface{} `json:"fields,omitempty"`
	Error         string                 `json:"error,omitempty"`
}

// StructuredLogger implements Logger interface using logrus
type StructuredLogger struct {
	logger        *logrus.Logger
	entry         *logrus.Entry
	serviceName   string
	correlationID string
}

// NewLogger creates a new structured logger with JSON formatting
// Requirement 8.7: Use JSON format for all log entries
func NewLogger(serviceName string) Logger {
	logger := logrus.New()
	logger.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: time.RFC3339Nano,
		FieldMap: logrus.FieldMap{
			logrus.FieldKeyTime:  "timestamp",
			logrus.FieldKeyLevel: "level",
			logrus.FieldKeyMsg:   "message",
		},
	})
	
	return &StructuredLogger{
		logger:      logger,
		entry:       logger.WithField("service", serviceName),
		serviceName: serviceName,
	}
}

// WithCorrelationID returns a new logger with correlation ID
// Requirements 8.1, 8.2, 8.3, 8.4, 8.6: Include correlation ID in all log entries
func (l *StructuredLogger) WithCorrelationID(id string) Logger {
	return &StructuredLogger{
		logger:        l.logger,
		entry:         l.entry.WithField("correlation_id", id),
		serviceName:   l.serviceName,
		correlationID: id,
	}
}

// WithFields returns a new logger with additional fields
func (l *StructuredLogger) WithFields(fields map[string]interface{}) Logger {
	return &StructuredLogger{
		logger:        l.logger,
		entry:         l.entry.WithFields(fields),
		serviceName:   l.serviceName,
		correlationID: l.correlationID,
	}
}

// WithField returns a new logger with an additional field
func (l *StructuredLogger) WithField(key string, value interface{}) Logger {
	return &StructuredLogger{
		logger:        l.logger,
		entry:         l.entry.WithField(key, value),
		serviceName:   l.serviceName,
		correlationID: l.correlationID,
	}
}

// Info logs an info message
func (l *StructuredLogger) Info(msg string) {
	l.entry.Info(msg)
}

// Error logs an error message with error context
// Requirement 8.6: Log all errors with correlation ID, timestamp, service name, and full error context
func (l *StructuredLogger) Error(msg string, err error) {
	if err != nil {
		l.entry.WithField("error", err.Error()).Error(msg)
	} else {
		l.entry.Error(msg)
	}
}

// Warn logs a warning message
func (l *StructuredLogger) Warn(msg string) {
	l.entry.Warn(msg)
}

// Debug logs a debug message
func (l *StructuredLogger) Debug(msg string) {
	l.entry.Debug(msg)
}

// SetLevel sets the logging level
func (l *StructuredLogger) SetLevel(level string) {
	switch level {
	case "debug":
		l.logger.SetLevel(logrus.DebugLevel)
	case "info":
		l.logger.SetLevel(logrus.InfoLevel)
	case "warn":
		l.logger.SetLevel(logrus.WarnLevel)
	case "error":
		l.logger.SetLevel(logrus.ErrorLevel)
	default:
		l.logger.SetLevel(logrus.InfoLevel)
	}
}

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

const (
	// CorrelationIDKey is the context key for correlation ID
	CorrelationIDKey contextKey = "correlation_id"
	// LoggerKey is the context key for logger
	LoggerKey contextKey = "logger"
)

// WithCorrelationIDContext adds correlation ID to context
func WithCorrelationIDContext(ctx context.Context, correlationID string) context.Context {
	return context.WithValue(ctx, CorrelationIDKey, correlationID)
}

// GetCorrelationIDFromContext retrieves correlation ID from context
func GetCorrelationIDFromContext(ctx context.Context) (string, bool) {
	correlationID, ok := ctx.Value(CorrelationIDKey).(string)
	return correlationID, ok
}

// WithLoggerContext adds logger to context
func WithLoggerContext(ctx context.Context, logger Logger) context.Context {
	return context.WithValue(ctx, LoggerKey, logger)
}

// GetLoggerFromContext retrieves logger from context
func GetLoggerFromContext(ctx context.Context) (Logger, bool) {
	logger, ok := ctx.Value(LoggerKey).(Logger)
	return logger, ok
}
