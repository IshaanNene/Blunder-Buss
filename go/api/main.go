/*
 * Copyright (c) 2025 Ishaan Nene
 *
 * This source code is licensed under the MIT license found in the
 * LICENSE file in the root directory of this source tree.
 */
/*
This file consists of Important API definitions and functions to handle incoming requests.
There are some important strucuts as well like MoveRequest, MoveResponse, Job, JobResult.
*/
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"blunderbuss/pkg/circuitbreaker"
	"blunderbuss/pkg/correlation"
	"blunderbuss/pkg/logging"
	"blunderbuss/pkg/metrics"
	"blunderbuss/pkg/retry"
)

var (
	rdb                 *redis.Client
	ctx                 = context.Background()
	metricsCollector    *metrics.MetricsCollector
	logger              logging.Logger
	correlationIDGen    *correlation.IDGenerator
	redisCircuitBreaker *circuitbreaker.CircuitBreaker
	retryConfig         retry.Config
	
	// Queue depth variance tracking (Requirement 5.7)
	queueDepthHistory []queueDepthSnapshot
	queueDepthMu      sync.RWMutex
)

type queueDepthSnapshot struct {
	timestamp time.Time
	depth     int64
}

type MoveRequest struct {
	FEN        string `json:"fen"`
	Elo        int    `json:"elo"`
	MoveTimeMs int    `json:"movetime_ms"`
}

type MoveResponse struct {
	BestMove string `json:"bestmove"`
	Ponder   string `json:"ponder,omitempty"`
	Info     string `json:"info,omitempty"`
}

// ErrorResponse represents an error response with circuit breaker details
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
	Code              string                 `json:"code"`
	Message           string                 `json:"message"`
	CorrelationID     string                 `json:"correlation_id"`
	RetryAfterSeconds int                    `json:"retry_after_seconds,omitempty"`
	Details           map[string]interface{} `json:"details,omitempty"`
}

type Job struct {
	JobID         string `json:"job_id"`
	CorrelationID string `json:"correlation_id"`
	FEN           string `json:"fen"`
	Elo           int    `json:"elo"`
	MaxTime       int    `json:"max_time_ms"`
	CreatedAt     string `json:"created_at"`
}

type JobResult struct {
	JobID         string `json:"job_id"`
	CorrelationID string `json:"correlation_id"`
	BestMove      string `json:"bestmove"`
	Ponder        string `json:"ponder,omitempty"`
	Info          string `json:"info,omitempty"`
	Error         string `json:"error,omitempty"`
}

// HealthStatus represents the health check response
type HealthStatus struct {
	Status         string `json:"status"`
	RedisConnected bool   `json:"redis_connected"`
	QueueDepth     int64  `json:"queue_depth"`
	Timestamp      string `json:"timestamp"`
}

func main() {
	// Initialize structured logger (requirement 8.7)
	logger = logging.NewLogger("api")
	logger.Info("Starting API service")

	// Initialize metrics collector (requirement 1.8)
	metricsCollector = metrics.NewMetricsCollector("api")
	logger.Info("Metrics collector initialized")

	// Initialize correlation ID generator (requirement 8.1)
	correlationIDGen = correlation.NewIDGenerator("api")

	// Initialize Redis circuit breaker (requirement 3.6)
	redisCircuitBreaker = circuitbreaker.New("redis", circuitbreaker.RedisCircuitBreakerConfig())
	redisCircuitBreaker.OnStateChange(func(from, to circuitbreaker.State) {
		logger.WithFields(map[string]interface{}{
			"from": from.String(),
			"to":   to.String(),
		}).Info("Redis circuit breaker state changed")
		
		// Update circuit breaker state metrics (requirement 3.8)
		var stateValue float64
		switch to {
		case circuitbreaker.StateClosed:
			stateValue = 0
		case circuitbreaker.StateHalfOpen:
			stateValue = 1
		case circuitbreaker.StateOpen:
			stateValue = 2
		}
		metricsCollector.SetCircuitBreakerState("redis", "api", stateValue)
	})

	// Initialize retry config (requirement 4.3)
	retryConfig = retry.RedisPublishRetryConfig()

	// Initialize Redis client
	redisAddr := getenv("REDIS_ADDR", "redis:6379")
	rdb = redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		logger.Error("Warning: Redis connection failed", err)
	} else {
		logger.WithField("redis_addr", redisAddr).Info("Connected to Redis")
	}

	// Create HTTP server with graceful shutdown
	mux := http.NewServeMux()
	
	// Metrics endpoint (requirement 1.8)
	mux.Handle("/metrics", promhttp.Handler())
	
	// Enhanced health check endpoint (requirement 6.1)
	mux.HandleFunc("/healthz", healthCheckHandler)
	
	// Move endpoint with correlation ID middleware
	mux.HandleFunc("/move", correlationIDMiddleware(moveHandler))

	addr := ":8080"
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// Start server in goroutine
	go func() {
		logger.WithFields(map[string]interface{}{
			"addr":       addr,
			"redis_addr": redisAddr,
		}).Info("API listening")
		
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Server error", err)
			os.Exit(1)
		}
	}()
	
	// Start queue depth variance tracking (Requirement 5.7)
	go trackQueueDepthVariance()

	// Graceful shutdown (requirement 6.6)
	gracefulShutdown(server)
}

// correlationIDMiddleware generates or extracts correlation ID (requirement 8.1, 8.2)
func correlationIDMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract or generate correlation ID
		correlationID := r.Header.Get("X-Correlation-ID")
		if correlationID == "" {
			correlationID = correlationIDGen.Generate()
		}

		// Store in context (requirement 8.2)
		ctx := correlation.WithID(r.Context(), correlationID)
		r = r.WithContext(ctx)

		// Add to response headers (requirement 8.5)
		w.Header().Set("X-Correlation-ID", correlationID)

		// Call next handler
		next(w, r)
	}
}

// healthCheckHandler implements enhanced health check (requirement 6.1, 6.4)
func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	// Check Redis connectivity with 2s timeout (requirement 6.1)
	checkCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	redisOk := rdb.Ping(checkCtx).Err() == nil
	queueDepth := int64(0)

	if redisOk {
		queueDepth, _ = rdb.LLen(checkCtx, "stockfish:jobs").Result()
	}

	status := "healthy"
	statusCode := http.StatusOK
	if !redisOk {
		status = "unhealthy"
		statusCode = http.StatusServiceUnavailable // requirement 6.4
	}

	healthStatus := HealthStatus{
		Status:         status,
		RedisConnected: redisOk,
		QueueDepth:     queueDepth,
		Timestamp:      time.Now().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(healthStatus)
}

// moveHandler handles chess move requests with full observability
func moveHandler(w http.ResponseWriter, r *http.Request) {
	// Get correlation ID from context
	correlationID, _ := correlation.FromContext(r.Context())
	reqLogger := logger.WithCorrelationID(correlationID)

	// Start latency tracking (requirement 1.1)
	startTime := time.Now()

	// Log request start (requirement 8.6)
	reqLogger.WithFields(map[string]interface{}{
		"method": r.Method,
		"path":   r.URL.Path,
	}).Info("Request started")

	enableCORS(&w, r)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		// Log request completion for OPTIONS (requirement 8.6, 8.8)
		duration := time.Since(startTime)
		reqLogger.WithFields(map[string]interface{}{
			"status":      204,
			"duration_ms": duration.Milliseconds(),
		}).Info("Request completed")
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		metricsCollector.IncrementRequestCounter("405")
		// Log request completion with error (requirement 8.6, 8.7, 8.8)
		duration := time.Since(startTime)
		reqLogger.WithFields(map[string]interface{}{
			"status":      405,
			"duration_ms": duration.Milliseconds(),
			"error":       "method not allowed",
		}).Error("Request completed with error", nil)
		metricsCollector.RecordRequestDuration("/move", "405", duration)
		return
	}

	var req MoveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Log error with full context (requirement 8.6, 8.7)
		duration := time.Since(startTime)
		reqLogger.WithFields(map[string]interface{}{
			"status":      400,
			"duration_ms": duration.Milliseconds(),
		}).Error("Bad JSON in request", err)
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		metricsCollector.IncrementRequestCounter("400")
		metricsCollector.RecordRequestDuration("/move", "400", duration)
		return
	}
	if req.FEN == "" {
		// Log error with full context (requirement 8.6, 8.7)
		duration := time.Since(startTime)
		reqLogger.WithFields(map[string]interface{}{
			"status":      400,
			"duration_ms": duration.Milliseconds(),
		}).Error("Missing FEN in request", nil)
		http.Error(w, "missing fen", http.StatusBadRequest)
		metricsCollector.IncrementRequestCounter("400")
		metricsCollector.RecordRequestDuration("/move", "400", duration)
		return
	}

	// Validate and set defaults
	if req.Elo == 0 {
		req.Elo = 1600
	}
	if req.Elo < 1320 {
		req.Elo = 1320
	}
	if req.Elo > 3190 {
		req.Elo = 3190
	}
	if req.MoveTimeMs <= 0 {
		req.MoveTimeMs = 1000
	}

	jobID := fmt.Sprintf("job_%d_%d", time.Now().UnixNano(), req.Elo)
	job := Job{
		JobID:         jobID,
		CorrelationID: correlationID,
		FEN:           req.FEN,
		Elo:           req.Elo,
		MaxTime:       req.MoveTimeMs,
		CreatedAt:     time.Now().Format(time.RFC3339Nano),
	}

	reqLogger.WithFields(map[string]interface{}{
		"job_id":       jobID,
		"elo":          req.Elo,
		"movetime_ms":  req.MoveTimeMs,
		"fen_preview":  truncateString(req.FEN, 20),
	}).Info("Processing move request")

	jobData, err := json.Marshal(job)
	if err != nil {
		// Log error with full context (requirement 8.6, 8.7)
		duration := time.Since(startTime)
		reqLogger.WithFields(map[string]interface{}{
			"job_id":      jobID,
			"status":      500,
			"duration_ms": duration.Milliseconds(),
		}).Error("Error marshaling job", err)
		http.Error(w, "failed to serialize job", http.StatusInternalServerError)
		metricsCollector.IncrementRequestCounter("500")
		metricsCollector.RecordRequestDuration("/move", "500", duration)
		return
	}

	// Publish job to Redis with circuit breaker and retry (requirements 3.6, 4.3)
	err = publishJobWithRetry(r.Context(), jobData, correlationID, reqLogger)
	if err != nil {
		duration := time.Since(startTime)
		// Check if circuit breaker is open (requirement 3.7)
		if redisCircuitBreaker.IsOpen() {
			sendCircuitBreakerError(w, correlationID, reqLogger)
			metricsCollector.IncrementRequestCounter("503")
			metricsCollector.RecordRequestDuration("/move", "503", duration)
			return
		}

		// Log error with full context (requirement 8.6, 8.7)
		reqLogger.WithFields(map[string]interface{}{
			"job_id":      jobID,
			"status":      503,
			"duration_ms": duration.Milliseconds(),
		}).Error("Error queuing job", err)
		http.Error(w, "failed to queue job: "+err.Error(), http.StatusServiceUnavailable)
		metricsCollector.IncrementRequestCounter("503")
		metricsCollector.RecordRequestDuration("/move", "503", duration)
		return
	}

	// Update queue depth metric (requirement 1.6)
	queueDepth, _ := rdb.LLen(r.Context(), "stockfish:jobs").Result()
	metricsCollector.SetQueueDepth(float64(queueDepth))

	timeout := time.Duration(req.MoveTimeMs+5000) * time.Millisecond
	result, err := waitForResult(jobID, timeout, correlationID, reqLogger)
	if err != nil {
		// Log error with full context (requirement 8.6, 8.7, 8.8)
		duration := time.Since(startTime)
		reqLogger.WithFields(map[string]interface{}{
			"job_id":      jobID,
			"status":      408,
			"duration_ms": duration.Milliseconds(),
			"timeout_ms":  timeout.Milliseconds(),
		}).Error("Job failed or timed out", err)
		http.Error(w, "job timeout or error: "+err.Error(), http.StatusRequestTimeout)
		metricsCollector.IncrementRequestCounter("408")
		metricsCollector.RecordRequestDuration("/move", "408", duration)
		return
	}

	// Record successful operation (requirement 5.1)
	metricsCollector.IncrementSuccessfulOps()

	// Log request completion (requirement 8.6, 8.8)
	duration := time.Since(startTime)
	reqLogger.WithFields(map[string]interface{}{
		"job_id":      jobID,
		"bestmove":    result.BestMove,
		"status":      200,
		"duration_ms": duration.Milliseconds(),
	}).Info("Request completed")

	// Record metrics (requirement 1.5, 1.6)
	metricsCollector.IncrementRequestCounter("200")
	metricsCollector.RecordRequestDuration("/move", "200", duration)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(MoveResponse{
		BestMove: result.BestMove,
		Ponder:   result.Ponder,
		Info:     result.Info,
	})
}

// publishJobWithRetry publishes job to Redis with circuit breaker and retry logic
func publishJobWithRetry(ctx context.Context, jobData []byte, correlationID string, reqLogger logging.Logger) error {
	var attemptNum int
	
	// Wrap with retry logic (requirement 4.3, 4.6)
	err := retry.WithRetry(ctx, retryConfig, func() error {
		attemptNum++
		
		// Log retry attempts (requirement 4.6)
		if attemptNum > 1 {
			reqLogger.WithFields(map[string]interface{}{
				"attempt":        attemptNum,
				"max_attempts":   retryConfig.MaxAttempts,
			}).Info("Retrying Redis job publish")
			
			// Increment retry metrics (requirement 4.6)
			metricsCollector.IncrementRetryAttempts("api", "redis_publish", strconv.Itoa(attemptNum))
		}
		
		// Wrap with circuit breaker (requirement 3.6)
		return redisCircuitBreaker.Call(func() error {
			err := rdb.LPush(ctx, "stockfish:jobs", jobData).Err()
			if err != nil {
				// Increment circuit breaker failure metrics (requirement 3.8)
				metricsCollector.IncrementCircuitBreakerFailures("redis", "api")
			}
			return err
		})
	})
	
	return err
}

// sendCircuitBreakerError sends HTTP 503 with retry-after header (requirement 3.7)
func sendCircuitBreakerError(w http.ResponseWriter, correlationID string, reqLogger logging.Logger) {
	reqLogger.Warn("Redis circuit breaker is open")
	
	cbMetrics := redisCircuitBreaker.Metrics()
	
	errorResp := ErrorResponse{
		Error: ErrorDetail{
			Code:              "SERVICE_UNAVAILABLE",
			Message:           "Redis temporarily unavailable",
			CorrelationID:     correlationID,
			RetryAfterSeconds: 30,
			Details: map[string]interface{}{
				"circuit_breaker_state": "open",
				"failure_count":         cbMetrics.ConsecutiveFails,
			},
		},
	}
	
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After", "30")
	w.WriteHeader(http.StatusServiceUnavailable)
	json.NewEncoder(w).Encode(errorResp)
}

func waitForResult(jobID string, timeout time.Duration, correlationID string, reqLogger logging.Logger) (*JobResult, error) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		results, err := rdb.LRange(ctx, "stockfish:results", 0, -1).Result()
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		for _, resultStr := range results {
			var result JobResult
			if err := json.Unmarshal([]byte(resultStr), &result); err != nil {
				continue
			}

			if result.JobID == jobID {
				rdb.LRem(ctx, "stockfish:results", 1, resultStr)

				if result.Error != "" {
					return nil, fmt.Errorf("engine error: %s", result.Error)
				}
				return &result, nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	return nil, fmt.Errorf("timeout waiting for job result")
}

// gracefulShutdown implements graceful shutdown (requirement 6.6)
func gracefulShutdown(server *http.Server) {
	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	
	sig := <-sigChan
	logger.WithField("signal", sig.String()).Info("Received shutdown signal")
	
	// Create shutdown context with 30s timeout (requirement 6.6)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	// Stop accepting new requests and wait for in-flight requests
	logger.Info("Shutting down server gracefully")
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("Server shutdown error", err)
	}
	
	// Close Redis connection cleanly (requirement 6.6)
	if rdb != nil {
		if err := rdb.Close(); err != nil {
			logger.Error("Redis close error", err)
		} else {
			logger.Info("Redis connection closed")
		}
	}
	
	logger.Info("Server stopped")
}

func enableCORS(w *http.ResponseWriter, r *http.Request) {
	(*w).Header().Set("Access-Control-Allow-Origin", getenv("CORS_ALLOW_ORIGIN", "*"))
	(*w).Header().Set("Access-Control-Allow-Headers", "Content-Type")
	(*w).Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	(*w).Header().Set("Content-Type", "application/json")
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// trackQueueDepthVariance periodically tracks queue depth and calculates variance
// Requirement 5.7: Calculate standard deviation of queue depth over time windows
func trackQueueDepthVariance() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	
	for {
		<-ticker.C
		updateQueueDepthVariance()
	}
}

// updateQueueDepthVariance updates queue depth history and calculates variance
func updateQueueDepthVariance() {
	// Get current queue depth
	queueDepth, err := rdb.LLen(ctx, "stockfish:jobs").Result()
	if err != nil {
		return
	}
	
	// Add to history
	queueDepthMu.Lock()
	snapshot := queueDepthSnapshot{
		timestamp: time.Now(),
		depth:     queueDepth,
	}
	queueDepthHistory = append(queueDepthHistory, snapshot)
	
	// Keep only last hour of data (240 samples at 15s intervals)
	if len(queueDepthHistory) > 240 {
		queueDepthHistory = queueDepthHistory[len(queueDepthHistory)-240:]
	}
	queueDepthMu.Unlock()
	
	// Calculate variance over different time windows
	calculateAndExposeVariance(5 * time.Minute)   // 5-minute window
	calculateAndExposeVariance(15 * time.Minute)  // 15-minute window
	calculateAndExposeVariance(60 * time.Minute)  // 1-hour window
}

// calculateAndExposeVariance calculates standard deviation for a time window
func calculateAndExposeVariance(window time.Duration) {
	queueDepthMu.RLock()
	defer queueDepthMu.RUnlock()
	
	if len(queueDepthHistory) == 0 {
		return
	}
	
	cutoff := time.Now().Add(-window)
	
	// Collect samples within window
	var samples []float64
	for _, snapshot := range queueDepthHistory {
		if snapshot.timestamp.After(cutoff) {
			samples = append(samples, float64(snapshot.depth))
		}
	}
	
	if len(samples) < 2 {
		return
	}
	
	// Calculate mean
	var sum float64
	for _, value := range samples {
		sum += value
	}
	mean := sum / float64(len(samples))
	
	// Calculate variance
	var varianceSum float64
	for _, value := range samples {
		diff := value - mean
		varianceSum += diff * diff
	}
	variance := varianceSum / float64(len(samples))
	
	// Calculate standard deviation
	stdDev := math.Sqrt(variance)
	
	// Expose metric (Requirement 5.7)
	// We use the standard deviation as the variance metric
	metricsCollector.SetQueueDepthVariance(stdDev)
}
