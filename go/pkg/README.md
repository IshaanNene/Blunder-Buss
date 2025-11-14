# Shared Go Packages for Observability Infrastructure

This directory contains shared Go packages used across the Blunder-Buss chess platform services for observability, fault tolerance, and structured logging.

## Packages

### metrics

Provides Prometheus metrics collection for latency tracking, counters, and gauges.

**Key Features:**
- API request duration histograms with P50/P95/P99 percentiles
- Worker job processing metrics (queue wait, engine connection, computation time)
- Circuit breaker state metrics
- Retry attempt tracking
- Cost efficiency metrics
- Latency tracker with microsecond precision

**Usage:**
```go
import "stockfish-scale/pkg/metrics"

// Create metrics collector for API service
mc := metrics.NewMetricsCollector("api")

// Record request duration
mc.RecordRequestDuration("/analyze", "200", duration)

// Track latency with checkpoints
tracker := metrics.NewLatencyTracker(correlationID)
tracker.Checkpoint("queue_insert")
// ... do work ...
duration := tracker.GetDuration()
```

### circuitbreaker

Wraps sony/gobreaker with custom metrics integration and predefined configurations.

**Key Features:**
- State machine: Closed → Open → Half-Open → Closed
- Configurable failure thresholds and timeouts
- Metrics integration for state tracking
- Predefined configs for Stockfish and Redis connections

**Usage:**
```go
import "stockfish-scale/pkg/circuitbreaker"

// Create circuit breaker for Stockfish connections
cb := circuitbreaker.New("stockfish", circuitbreaker.StockfishCircuitBreakerConfig())

// Execute with circuit breaker protection
err := cb.Call(func() error {
    return connectToStockfish()
})

if err == circuitbreaker.ErrCircuitOpen {
    // Circuit is open, fail fast
}
```

### retry

Implements exponential backoff with jitter for retry logic.

**Key Features:**
- Exponential backoff with configurable multiplier
- Jitter to prevent thundering herd
- Context-aware cancellation
- Predefined configs for different use cases

**Usage:**
```go
import "stockfish-scale/pkg/retry"

// Retry Stockfish connection with exponential backoff
err := retry.WithRetry(ctx, retry.StockfishRetryConfig(), func() error {
    return connectToStockfish()
})

if err != nil {
    // All retry attempts exhausted
}
```

### logging

Provides structured JSON logging with correlation ID support using logrus.

**Key Features:**
- JSON formatted log entries
- Correlation ID propagation
- Context-aware logging
- Field-based structured logging

**Usage:**
```go
import "stockfish-scale/pkg/logging"

// Create logger
logger := logging.NewLogger("api")

// Add correlation ID
logger = logger.WithCorrelationID(correlationID)

// Log with additional fields
logger.WithFields(map[string]interface{}{
    "job_id": jobID,
    "duration_ms": duration.Milliseconds(),
}).Info("Job completed")

// Log errors with context
logger.Error("Failed to connect", err)
```

### correlation

Utilities for correlation ID generation and context propagation.

**Key Features:**
- Unique correlation ID generation
- Context propagation utilities
- Header extraction helpers
- Format: `{service}-{timestamp}-{random}`

**Usage:**
```go
import "stockfish-scale/pkg/correlation"

// Create ID generator
generator := correlation.NewIDGenerator("api")

// Generate new correlation ID
correlationID := generator.Generate()

// Add to context
ctx = correlation.WithID(ctx, correlationID)

// Retrieve from context
if id, ok := correlation.FromContext(ctx); ok {
    // Use correlation ID
}

// Extract from HTTP header
correlationID := correlation.ExtractFromHeader(r.Header.Get("X-Correlation-ID"))
```

## Requirements Mapping

These packages implement the following requirements from the observability specification:

- **Requirements 1.1-1.8**: Latency tracking and metrics collection
- **Requirements 3.1-3.8**: Circuit breaker implementation
- **Requirements 4.1-4.7**: Retry logic with exponential backoff
- **Requirements 8.1-8.8**: Structured logging and correlation ID propagation

## Dependencies

- `github.com/prometheus/client_golang` - Prometheus metrics client
- `github.com/sirupsen/logrus` - Structured logging
- `github.com/sony/gobreaker` - Circuit breaker implementation

## Building

```bash
cd go/pkg
go mod tidy
go build ./...
```

## Testing

```bash
cd go/pkg
go test ./...
```
