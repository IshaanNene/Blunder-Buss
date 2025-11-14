/*
 * Copyright (c) 2025 Ishaan Nene
 *
 * This source code is licensed under the MIT license found in the
 * LICENSE file in the root directory of this source tree.
 */
/*
This file consists of the worker code that processes jobs from the Redis queue, interacts with the chess engine, and publishes results back to Redis.
Please note: the struct definitions for Job and JobResult are repeated here for clarity, even though they are also defined in api/main.go.
Dont change the definitions
*/
package main

import (
    "bufio"
    "context"
    "encoding/json"
    "fmt"
    "io/ioutil"
    "net"
    "net/http"
    "os"
    "os/signal"
    "strconv"
    "strings"
    "sync"
    "sync/atomic"
    "syscall"
    "time"
    
    "github.com/go-redis/redis/v8"
    "github.com/prometheus/client_golang/prometheus/promhttp"
    "github.com/sony/gobreaker"
    
    "stockfish-scale/pkg/circuitbreaker"
    "stockfish-scale/pkg/correlation"
    "stockfish-scale/pkg/k8s"
    "stockfish-scale/pkg/logging"
    "stockfish-scale/pkg/metrics"
    "stockfish-scale/pkg/retry"
)

var (
    rdb           *redis.Client
    ctx           = context.Background()
    metricsCol    *metrics.MetricsCollector
    logger        logging.Logger
    stockfishCB   *gobreaker.CircuitBreaker
    activeJobsCount int32
    shutdownChan  chan struct{}
    doneChan      chan struct{}
    
    // Cost efficiency tracking (Requirement 5.2, 5.3)
    totalOperations int64
    lastCPUTime     time.Duration
    cpuTrackingMu   sync.Mutex
    
    // Idle time tracking (Requirement 5.5)
    totalIdleTime     time.Duration
    totalProcessTime  time.Duration
    idleTrackingMu    sync.Mutex
    workerStartTime   time.Time
)

type Job struct {
    JobID        string `json:"job_id"`
    CorrelationID string `json:"correlation_id,omitempty"` // Requirement 8.3: Extract correlation ID
    FEN          string `json:"fen"`
    Elo          int    `json:"elo"`
    MaxTime      int    `json:"max_time_ms"`
    CreatedAt    string `json:"created_at,omitempty"` // Requirement 1.2: Job creation timestamp
}

type JobResult struct {
    JobID         string            `json:"job_id"`
    CorrelationID string            `json:"correlation_id,omitempty"` // Requirement 8.4: Add correlation ID to result
    BestMove      string            `json:"bestmove"`
    Ponder        string            `json:"ponder,omitempty"`
    Info          string            `json:"info,omitempty"`
    Error         string            `json:"error,omitempty"`
    Timings       map[string]int64  `json:"timings,omitempty"` // Requirement 1.2, 1.3, 1.4: Add timing data
    CompletedAt   string            `json:"completed_at,omitempty"`
}

func main() {
    // Initialize structured logger (Requirement 8.6, 8.7)
    logger = logging.NewLogger("worker")
    logger.Info("Worker service starting")
    
    // Initialize metrics collector (Requirement 1.2, 1.3, 1.4, 1.7, 1.8)
    metricsCol = metrics.NewMetricsCollector("worker")
    
    // Get configuration from environment
    redisAddr := getenv("REDIS_ADDR", "redis:6379")
    engineAddr := getenv("ENGINE_ADDR", "stockfish:4000")
    metricsPort := getenv("METRICS_PORT", "9090")
    
    // Initialize Redis client
    rdb = redis.NewClient(&redis.Options{Addr: redisAddr})
    _, err := rdb.Ping(ctx).Result()
    if err != nil {
        logger.Error("Redis connection failed", err)
    } else {
        logger.WithField("redis_addr", redisAddr).Info("Connected to Redis")
    }
    
    // Initialize circuit breaker for Stockfish connections (Requirement 3.1-3.5)
    stockfishCB = circuitbreaker.NewStockfishCircuitBreaker(metricsCol)
    
    // Initialize shutdown channels (Requirement 6.7)
    shutdownChan = make(chan struct{})
    doneChan = make(chan struct{})
    
    // Start HTTP server for metrics and health endpoints (Requirement 3.7)
    go startHTTPServer(metricsPort, engineAddr)
    
    // Setup graceful shutdown (Requirement 6.7)
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)
    
    go func() {
        <-sigChan
        logger.Info("Shutdown signal received")
        close(shutdownChan)
    }()
    
    logger.WithFields(map[string]interface{}{
        "redis_addr":   redisAddr,
        "engine_addr":  engineAddr,
        "metrics_port": metricsPort,
    }).Info("Worker started")
    
    // Initialize worker start time for idle percentage calculation (Requirement 5.5)
    workerStartTime = time.Now()
    
    // Start CPU tracking goroutine (Requirement 5.2, 5.3)
    go trackCPUAndEfficiency()
    
    // Start idle percentage tracking goroutine (Requirement 5.5)
    go trackIdlePercentage()
    
    // Start replica tracking (Requirement 5.4)
    replicaTracker, err := k8s.NewReplicaTracker(metricsCol, logger)
    if err != nil {
        logger.WithField("error", err.Error()).Warn("Failed to create replica tracker")
    } else if replicaTracker != nil {
        replicaTracker.Start()
        defer replicaTracker.Stop()
    }
    
    // Main job processing loop
    processJobs(engineAddr)
    
    // Wait for graceful shutdown to complete
    <-doneChan
    
    // Close Redis connection cleanly (Requirement 6.7)
    if err := rdb.Close(); err != nil {
        logger.Error("Error closing Redis connection", err)
    } else {
        logger.Info("Redis connection closed")
    }
    
    logger.Info("Worker shutdown complete")
}

// startHTTPServer starts the HTTP server for metrics and health endpoints
// Requirement 3.7: Create HTTP server on port 9090 for health and metrics
func startHTTPServer(port, engineAddr string) {
    mux := http.NewServeMux()
    
    // Metrics endpoint (Requirement 1.8)
    mux.Handle("/metrics", promhttp.Handler())
    
    // Health check endpoint (Requirement 6.2, 6.3, 6.5)
    mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
        healthCheck(w, r, engineAddr)
    })
    
    addr := ":" + port
    logger.WithField("port", port).Info("Starting HTTP server for metrics and health")
    
    if err := http.ListenAndServe(addr, mux); err != nil {
        logger.Error("HTTP server failed", err)
    }
}

// processJobs is the main job processing loop
func processJobs(engineAddr string) {
    idleStart := time.Now()
    
    for {
        select {
        case <-shutdownChan:
            logger.Info("Stopping job processing loop")
            
            // Track final idle period before shutdown (Requirement 5.5)
            if time.Since(idleStart) > 0 {
                idleDuration := time.Since(idleStart)
                metricsCol.IncrementIdleTime(idleDuration)
                recordIdleTime(idleDuration)
            }
            
            // Wait for current job to complete with 30s timeout (Requirement 6.7)
            shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
            defer cancel()
            
            // Wait for active jobs to complete
            ticker := time.NewTicker(100 * time.Millisecond)
            defer ticker.Stop()
            
            for {
                if atomic.LoadInt32(&activeJobsCount) == 0 {
                    logger.Info("All jobs completed, shutting down")
                    close(doneChan)
                    return
                }
                
                select {
                case <-shutdownCtx.Done():
                    logger.Warn("Shutdown timeout reached, forcing exit")
                    close(doneChan)
                    return
                case <-ticker.C:
                    // Continue waiting
                }
            }
            
        default:
            // Try to get a job from Redis with timeout
            res, err := rdb.BLPop(ctx, 5*time.Second, "stockfish:jobs").Result()
            if err != nil {
                if err == redis.Nil {
                    // No jobs available, continue idle tracking (Requirement 5.5)
                    // We'll track idle time when we get a job or periodically
                    continue
                }
                logger.Error("Redis BLPOP error", err)
                time.Sleep(1 * time.Second)
                continue
            }
            
            // Job received - record idle time (Requirement 5.5)
            idleDuration := time.Since(idleStart)
            if idleDuration > 0 {
                metricsCol.IncrementIdleTime(idleDuration)
                recordIdleTime(idleDuration)
            }
            
            if len(res) < 2 {
                // Reset idle timer for next iteration
                idleStart = time.Now()
                continue
            }
            
            var job Job
            if err := json.Unmarshal([]byte(res[1]), &job); err != nil {
                logger.Error("Invalid job payload", err)
                // Reset idle timer for next iteration
                idleStart = time.Now()
                continue
            }
            
            // Process job in goroutine
            // Note: We track processing time in handleJob
            go handleJob(engineAddr, job)
            
            // Reset idle timer for next iteration
            idleStart = time.Now()
        }
    }
}

// connectToStockfish connects to Stockfish with circuit breaker and retry logic
// Requirements 3.1-3.5: Circuit breaker protection with 5 failures in 60s threshold
// Requirements 4.1, 4.2, 4.5, 4.7: Retry with exponential backoff
func connectToStockfish(ctx context.Context, engineAddr string, jobLogger logging.Logger) (net.Conn, error) {
	var conn net.Conn
	var lastErr error
	var attemptCount int
	
	// Wrap Stockfish TCP dial operations with circuit breaker (Requirement 3.3)
	_, err := stockfishCB.Execute(func() (interface{}, error) {
		// Use retry logic with exponential backoff (Requirement 4.1, 4.2, 4.5, 4.7)
		retryCfg := retry.StockfishRetryConfig()
		
		// Add retry callback to log each retry attempt with backoff duration (Requirement 4.7)
		retryCfg.OnRetry = func(attempt int, delay time.Duration, err error) {
			jobLogger.WithFields(map[string]interface{}{
				"attempt":       attempt,
				"max_attempts":  retryCfg.MaxAttempts,
				"backoff_ms":    delay.Milliseconds(),
				"engine_addr":   engineAddr,
				"error":         err.Error(),
			}).Warn("Retrying Stockfish connection after backoff")
		}
		
		retryErr := retry.WithRetry(ctx, retryCfg, func() error {
			attemptCount++
			c, dialErr := net.DialTimeout("tcp", engineAddr, 5*time.Second)
			if dialErr != nil {
				lastErr = dialErr
				jobLogger.WithFields(map[string]interface{}{
					"engine_addr": engineAddr,
					"attempt":     attemptCount,
					"error":       dialErr.Error(),
				}).Warn("Stockfish connection attempt failed")
				return dialErr
			}
			conn = c
			return nil
		})
		
		return conn, retryErr
	})
	
	if err != nil {
		// Fail jobs immediately when circuit is open (Requirement 3.2, 3.3)
		if err == gobreaker.ErrOpenState {
			jobLogger.WithFields(map[string]interface{}{
				"circuit_state": "open",
				"service":       "stockfish",
			}).Error("Circuit breaker open, failing job immediately", err)
			return nil, fmt.Errorf("stockfish service temporarily unavailable (circuit breaker open, will retry in 30s)")
		}
		
		// Circuit breaker is closed or half-open, but connection failed after retries
		if err == gobreaker.ErrTooManyRequests {
			jobLogger.WithFields(map[string]interface{}{
				"circuit_state": "half-open",
				"service":       "stockfish",
			}).Error("Circuit breaker half-open, test connection failed", err)
			return nil, fmt.Errorf("stockfish service test connection failed (circuit breaker half-open)")
		}
		
		jobLogger.WithFields(map[string]interface{}{
			"engine_addr":   engineAddr,
			"total_attempts": attemptCount,
		}).Error("Failed to connect to Stockfish after retries", lastErr)
		return nil, fmt.Errorf("failed to connect to stockfish: %w", lastErr)
	}
	
	return conn, nil
}

func handleJob(engineAddr string, job Job) {
    // Track active jobs (Requirement 6.5)
    atomic.AddInt32(&activeJobsCount, 1)
    defer atomic.AddInt32(&activeJobsCount, -1)
    
    metricsCol.SetActiveJobs(float64(atomic.LoadInt32(&activeJobsCount)))
    
    // Start total processing timer (Requirement 1.7)
    processingStart := time.Now()
    
    // Extract correlation ID from job payload (Requirement 8.3)
    correlationID := job.CorrelationID
    if correlationID == "" {
        // Generate one if not present
        gen := correlation.NewIDGenerator("worker")
        correlationID = gen.Generate()
    }
    
    // Store correlation ID in goroutine context (Requirement 8.3)
    jobCtx := correlation.WithID(context.Background(), correlationID)
    
    // Create logger with correlation ID for all log entries (Requirement 8.6)
    jobLogger := logger.WithCorrelationID(correlationID).WithFields(map[string]interface{}{
        "job_id": job.JobID,
        "fen":    job.FEN,
        "elo":    job.Elo,
    })
    
    jobLogger.Info("Processing job")
    
    // Initialize result
    result := JobResult{
        JobID:         job.JobID,
        CorrelationID: correlationID, // Requirement 8.4: Add correlation ID to result
        Timings:       make(map[string]int64),
    }
    
    // Calculate queue wait time (Requirement 1.2)
    if job.CreatedAt != "" {
        createdAt, err := time.Parse(time.RFC3339Nano, job.CreatedAt)
        if err == nil {
            queueWait := time.Since(createdAt)
            result.Timings["queue_wait_ms"] = queueWait.Milliseconds()
            metricsCol.RecordQueueWaitTime(queueWait)
            
            jobLogger.WithField("queue_wait_ms", queueWait.Milliseconds()).Info("Job dequeued")
        }
    }
    
    // Connect to Stockfish with circuit breaker and retry (Requirements 3.3, 4.1-4.2, 4.5, 4.7)
    connectionStart := time.Now()
    conn, err := connectToStockfish(jobCtx, engineAddr, jobLogger)
    if err != nil {
        result.Error = fmt.Sprintf("engine connect error: %v", err)
        result.CompletedAt = time.Now().Format(time.RFC3339Nano)
        publishResult(jobCtx, result, jobLogger)
        
        // Record metrics
        metricsCol.RecordTotalProcessingTime(time.Since(processingStart))
        return
    }
    defer conn.Close()
    
    connectionDuration := time.Since(connectionStart)
    result.Timings["engine_connect_ms"] = connectionDuration.Milliseconds()
    metricsCol.RecordEngineConnectionTime(connectionDuration)
    
    jobLogger.WithField("connection_ms", connectionDuration.Milliseconds()).Info("Connected to engine")
    
    // Execute chess computation (Requirement 1.3)
    computeStart := time.Now()
    bestMove, ponder, info, err := executeChessComputation(conn, job, jobLogger)
    computeDuration := time.Since(computeStart)
    
    result.Timings["engine_compute_ms"] = computeDuration.Milliseconds()
    metricsCol.RecordEngineComputeTime(computeDuration)
    
    if err != nil {
        result.Error = fmt.Sprintf("engine computation error: %v", err)
    } else {
        result.BestMove = bestMove
        result.Ponder = ponder
        result.Info = info
    }
    
    result.CompletedAt = time.Now().Format(time.RFC3339Nano)
    
    // Publish result with retry (Requirement 4.4, 4.6)
    publishStart := time.Now()
    publishResult(jobCtx, result, jobLogger)
    publishDuration := time.Since(publishStart)
    
    result.Timings["result_publish_ms"] = publishDuration.Milliseconds()
    metricsCol.RecordResultPublishTime(publishDuration)
    
    // Record total processing time (Requirement 1.4, 8.8)
    totalDuration := time.Since(processingStart)
    result.Timings["total_ms"] = totalDuration.Milliseconds()
    metricsCol.RecordTotalProcessingTime(totalDuration)
    
    // Track processing time for idle percentage calculation (Requirement 5.5)
    recordProcessingTime(totalDuration)
    
    // Log structured entry with all timings (Requirement 8.8)
    jobLogger.WithFields(map[string]interface{}{
        "queue_wait_ms":     result.Timings["queue_wait_ms"],
        "engine_connect_ms": result.Timings["engine_connect_ms"],
        "engine_compute_ms": result.Timings["engine_compute_ms"],
        "result_publish_ms": result.Timings["result_publish_ms"],
        "total_ms":          result.Timings["total_ms"],
        "bestmove":          result.BestMove,
        "error":             result.Error,
    }).Info("Job completed")
    
    // Increment total operations for cost efficiency tracking (Requirement 5.3)
    if result.Error == "" {
        atomic.AddInt64(&totalOperations, 1)
    }
}
// executeChessComputation executes the chess computation on the Stockfish engine
func executeChessComputation(conn net.Conn, job Job, jobLogger logging.Logger) (string, string, string, error) {
    reader := bufio.NewReader(conn)
    
    write := func(cmd string) error {
        _, err := fmt.Fprintf(conn, "%s\n", cmd)
        return err
    }
    
    readUntil := func(substr string, timeout time.Duration) (string, error) {
        deadline := time.Now().Add(timeout)
        var lines []string
        
        for time.Now().Before(deadline) {
            conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
            line, err := reader.ReadString('\n')
            if err != nil {
                if ne, ok := err.(net.Error); ok && ne.Timeout() {
                    continue
                }
                return strings.Join(lines, "\n"), err
            }
            line = strings.TrimSpace(line)
            lines = append(lines, line)
            if strings.Contains(line, substr) {
                return strings.Join(lines, "\n"), nil
            }
        }
        return strings.Join(lines, "\n"), fmt.Errorf("timeout waiting for %q", substr)
    }
    
    // Initialize UCI
    if err := write("uci"); err != nil {
        return "", "", "", fmt.Errorf("uci command error: %v", err)
    }
    
    if _, err := readUntil("uciok", 3*time.Second); err != nil {
        return "", "", "", fmt.Errorf("uci init error: %v", err)
    }
    
    // Set ELO if specified
    if job.Elo > 0 {
        if err := write("setoption name UCI_LimitStrength value true"); err != nil {
            jobLogger.Warn("Failed to set limit strength")
        }
        if err := write(fmt.Sprintf("setoption name UCI_Elo value %d", job.Elo)); err != nil {
            jobLogger.Warn("Failed to set ELO")
        }
    }
    
    // Check if engine is ready
    if err := write("isready"); err != nil {
        return "", "", "", fmt.Errorf("isready command error: %v", err)
    }
    
    if _, err := readUntil("readyok", 2*time.Second); err != nil {
        return "", "", "", fmt.Errorf("ready check error: %v", err)
    }
    
    // Start new game
    if err := write("ucinewgame"); err != nil {
        return "", "", "", fmt.Errorf("ucinewgame command error: %v", err)
    }
    
    // Set position
    if strings.TrimSpace(job.FEN) == "" {
        write("position startpos")
    } else {
        write(fmt.Sprintf("position fen %s", job.FEN))
    }
    
    // Start computation
    moveTimeMs := job.MaxTime
    if moveTimeMs <= 0 {
        moveTimeMs = 1000
    }
    
    if err := write(fmt.Sprintf("go movetime %d", moveTimeMs)); err != nil {
        return "", "", "", fmt.Errorf("go command error: %v", err)
    }
    
    // Wait for bestmove
    deadline := time.Now().Add(time.Duration(moveTimeMs+5000) * time.Millisecond)
    var infoLines []string
    var bestMove, ponder string
    
    for time.Now().Before(deadline) {
        conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
        line, err := reader.ReadString('\n')
        if err != nil {
            if ne, ok := err.(net.Error); ok && ne.Timeout() {
                continue
            }
            return "", "", "", fmt.Errorf("engine read error: %v", err)
        }
        
        line = strings.TrimSpace(line)
        if line == "" {
            continue
        }
        
        if strings.HasPrefix(line, "info ") {
            infoLines = append(infoLines, line)
        } else if strings.HasPrefix(line, "bestmove ") {
            fields := strings.Fields(line)
            if len(fields) >= 2 {
                bestMove = fields[1]
            }
            if len(fields) >= 4 && fields[2] == "ponder" {
                ponder = fields[3]
            }
            
            if bestMove == "" {
                return "", "", "", fmt.Errorf("no bestmove received")
            }
            
            return bestMove, ponder, strings.Join(infoLines, "\n"), nil
        }
    }
    
    return "", "", "", fmt.Errorf("timeout waiting for bestmove")
}

func publishResult(jobCtx context.Context, result JobResult, jobLogger logging.Logger) {
    data, err := json.Marshal(result)
    if err != nil {
        jobLogger.Error("Error marshaling result", err)
        return
    }
    
    // Use retry logic for Redis result publishing (Requirement 4.4, 4.6)
    retryCfg := retry.RedisResultRetryConfig()
    
    // Add retry callback to log each retry attempt with backoff duration (Requirement 4.7)
    retryCfg.OnRetry = func(attempt int, delay time.Duration, err error) {
        jobLogger.WithFields(map[string]interface{}{
            "attempt":       attempt,
            "max_attempts":  retryCfg.MaxAttempts,
            "backoff_ms":    delay.Milliseconds(),
            "operation":     "redis_result_publish",
            "error":         err.Error(),
        }).Warn("Retrying Redis result publishing after backoff")
    }
    
    err = retry.WithRetry(jobCtx, retryCfg, func() error {
        return rdb.RPush(ctx, "stockfish:results", data).Err()
    })
    
    if err != nil {
        // Log failure after all retries exhausted (Requirement 4.6)
        jobLogger.WithFields(map[string]interface{}{
            "retry_attempts": retryCfg.MaxAttempts,
            "job_id":         result.JobID,
            "operation":      "redis_result_publish",
        }).Error("Failed to publish result after all retries exhausted", err)
        return
    }
    
    if result.Error != "" {
        jobLogger.WithField("error", result.Error).Warn("Job completed with error")
    } else {
        jobLogger.WithField("bestmove", result.BestMove).Info("Job completed successfully")
    }
}

// healthCheck implements the health check endpoint
// Requirement 6.2, 6.3, 6.5: Check Redis and Stockfish connectivity
func healthCheck(w http.ResponseWriter, r *http.Request, engineAddr string) {
    type HealthStatus struct {
        Status           string `json:"status"`
        RedisConnected   bool   `json:"redis_connected"`
        StockfishHealthy bool   `json:"stockfish_healthy"`
        CurrentJobs      int32  `json:"current_jobs"`
        Timestamp        string `json:"timestamp"`
    }
    
    // Check Redis connectivity with 2s timeout (Requirement 6.2)
    redisCtx, redisCancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer redisCancel()
    
    redisOk := rdb.Ping(redisCtx).Err() == nil
    
    // Check Stockfish connectivity with test connection (Requirement 6.3)
    stockfishOk := checkStockfishHealth(engineAddr, 2*time.Second)
    
    status := "healthy"
    statusCode := http.StatusOK
    if !redisOk || !stockfishOk {
        status = "unhealthy"
        statusCode = http.StatusServiceUnavailable
    }
    
    health := HealthStatus{
        Status:           status,
        RedisConnected:   redisOk,
        StockfishHealthy: stockfishOk,
        CurrentJobs:      atomic.LoadInt32(&activeJobsCount),
        Timestamp:        time.Now().Format(time.RFC3339),
    }
    
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(statusCode)
    json.NewEncoder(w).Encode(health)
}

// checkStockfishHealth verifies engine responsiveness within timeout
// Requirement 6.3: Verify engine responsiveness within 2 seconds
func checkStockfishHealth(engineAddr string, timeout time.Duration) bool {
    conn, err := net.DialTimeout("tcp", engineAddr, timeout)
    if err != nil {
        return false
    }
    defer conn.Close()
    
    // Set read/write deadline
    conn.SetDeadline(time.Now().Add(timeout))
    
    // Send UCI command and wait for "uciok" response
    _, err = conn.Write([]byte("uci\n"))
    if err != nil {
        return false
    }
    
    scanner := bufio.NewScanner(conn)
    for scanner.Scan() {
        if strings.Contains(scanner.Text(), "uciok") {
            return true
        }
    }
    
    return false
}

func getenv(k, def string) string {
    v := os.Getenv(k)
    if v == "" {
        return def
    }
    return v
}

// trackCPUAndEfficiency periodically tracks CPU usage and calculates cost efficiency
// Requirement 5.2: Track CPU-seconds consumed using container metrics
// Requirement 5.3: Calculate cost efficiency ratio (operations / CPU-seconds)
func trackCPUAndEfficiency() {
    ticker := time.NewTicker(10 * time.Second)
    defer ticker.Stop()
    
    // Initialize last CPU time
    cpuTrackingMu.Lock()
    lastCPUTime = getCPUTime()
    cpuTrackingMu.Unlock()
    
    for {
        select {
        case <-shutdownChan:
            return
        case <-ticker.C:
            updateCPUMetrics()
        }
    }
}

// updateCPUMetrics updates CPU consumption and cost efficiency metrics
func updateCPUMetrics() {
    cpuTrackingMu.Lock()
    defer cpuTrackingMu.Unlock()
    
    currentCPUTime := getCPUTime()
    if currentCPUTime == 0 {
        return
    }
    
    // Calculate CPU seconds consumed since last check
    cpuDelta := currentCPUTime - lastCPUTime
    if cpuDelta > 0 {
        cpuSeconds := cpuDelta.Seconds()
        metricsCol.IncrementCPUSeconds(cpuSeconds)
        
        // Calculate cost efficiency ratio (operations / CPU-seconds)
        // Requirement 5.3: Calculate cost efficiency ratio
        ops := atomic.LoadInt64(&totalOperations)
        if cpuSeconds > 0 && ops > 0 {
            // Calculate efficiency as operations per CPU-second
            totalCPUSeconds := currentCPUTime.Seconds()
            if totalCPUSeconds > 0 {
                efficiency := float64(ops) / totalCPUSeconds
                metricsCol.SetCostEfficiency(efficiency)
            }
        }
        
        lastCPUTime = currentCPUTime
    }
}

// recordIdleTime records idle time for percentage calculation
// Requirement 5.5: Track time spent waiting for jobs vs processing
func recordIdleTime(duration time.Duration) {
    idleTrackingMu.Lock()
    defer idleTrackingMu.Unlock()
    totalIdleTime += duration
}

// recordProcessingTime records processing time for percentage calculation
// Requirement 5.5: Track time spent waiting for jobs vs processing
func recordProcessingTime(duration time.Duration) {
    idleTrackingMu.Lock()
    defer idleTrackingMu.Unlock()
    totalProcessTime += duration
}

// trackIdlePercentage periodically calculates and exposes idle percentage
// Requirement 5.5: Calculate idle percentage and expose idle time metrics
func trackIdlePercentage() {
    ticker := time.NewTicker(10 * time.Second)
    defer ticker.Stop()
    
    for {
        select {
        case <-shutdownChan:
            return
        case <-ticker.C:
            calculateIdlePercentage()
        }
    }
}

// calculateIdlePercentage calculates the idle percentage
// Requirement 5.5: Calculate idle percentage
func calculateIdlePercentage() {
    idleTrackingMu.Lock()
    defer idleTrackingMu.Unlock()
    
    // Calculate total uptime
    totalUptime := time.Since(workerStartTime)
    if totalUptime == 0 {
        return
    }
    
    // Calculate idle percentage
    // Idle percentage = (idle time / total uptime) * 100
    idlePercentage := (float64(totalIdleTime) / float64(totalUptime)) * 100.0
    
    // Ensure percentage is between 0 and 100
    if idlePercentage < 0 {
        idlePercentage = 0
    } else if idlePercentage > 100 {
        idlePercentage = 100
    }
    
    // Expose idle percentage metric (Requirement 5.5)
    metricsCol.SetIdlePercentage(idlePercentage)
    
    // Log idle statistics periodically for debugging
    logger.WithFields(map[string]interface{}{
        "idle_percentage":    idlePercentage,
        "total_idle_seconds": totalIdleTime.Seconds(),
        "total_process_seconds": totalProcessTime.Seconds(),
        "total_uptime_seconds": totalUptime.Seconds(),
    }).Info("Idle time statistics")
}

// getCPUTime reads the current CPU time from /proc/self/stat
// This returns the total CPU time (user + system) consumed by the process
// Requirement 5.2: Track CPU-seconds consumed using container metrics
func getCPUTime() time.Duration {
    // Read /proc/self/stat which contains process CPU usage
    data, err := ioutil.ReadFile("/proc/self/stat")
    if err != nil {
        // If we can't read proc stats (e.g., not on Linux), return 0
        return 0
    }
    
    // Parse the stat file
    // Format: pid (comm) state ppid pgrp session tty_nr tpgid flags minflt cminflt majflt cmajflt utime stime ...
    // We need fields 14 (utime) and 15 (stime) which are in clock ticks
    fields := strings.Fields(string(data))
    if len(fields) < 15 {
        return 0
    }
    
    // Parse utime (user mode CPU time in clock ticks)
    utime, err := strconv.ParseInt(fields[13], 10, 64)
    if err != nil {
        return 0
    }
    
    // Parse stime (kernel mode CPU time in clock ticks)
    stime, err := strconv.ParseInt(fields[14], 10, 64)
    if err != nil {
        return 0
    }
    
    // Total CPU time in clock ticks
    totalTicks := utime + stime
    
    // Convert clock ticks to duration
    // Clock ticks per second is typically 100 (USER_HZ)
    clockTicksPerSecond := int64(100)
    
    // Try to read actual clock ticks per second from system
    if clkTck := os.Getenv("CLK_TCK"); clkTck != "" {
        if val, err := strconv.ParseInt(clkTck, 10, 64); err == nil && val > 0 {
            clockTicksPerSecond = val
        }
    }
    
    // Convert to nanoseconds
    nanoseconds := (totalTicks * 1e9) / clockTicksPerSecond
    return time.Duration(nanoseconds)
}