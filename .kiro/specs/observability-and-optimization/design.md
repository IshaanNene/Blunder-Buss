# Design Document: Observability and Optimization

## Overview

This design enhances the Blunder-Buss chess platform with production-grade observability, intelligent auto-scaling, fault tolerance, and cost optimization. The solution integrates Prometheus for metrics collection, implements circuit breakers and retry logic in Go services, adds structured logging with correlation IDs, and creates comprehensive Grafana dashboards for real-time monitoring.

The design follows cloud-native best practices and maintains backward compatibility with the existing architecture while adding minimal latency overhead (< 1ms per request).

## Architecture

### High-Level Component Diagram

```
┌─────────────┐
│   Client    │
└──────┬──────┘
       │ HTTP + X-Correlation-ID
       ▼
┌─────────────────────────────────────────┐
│         API Service (Enhanced)          │
│  ┌────────────────────────────────┐    │
│  │ Metrics: Latency, Throughput   │    │
│  │ Circuit Breaker: Redis         │    │
│  │ Retry Logic: Exponential       │    │
│  │ Structured Logging + Corr ID   │    │
│  └────────────────────────────────┘    │
│         /metrics (Prometheus)           │
└──────┬──────────────────────┬───────────┘
       │                      │
       │ Jobs                 │ Results
       ▼                      ▼
┌─────────────────────────────────────────┐
│         Redis Queue (Monitored)         │
│  - stockfish:jobs (depth tracked)       │
│  - stockfish:results                    │
└──────┬──────────────────────────────────┘
       │
       │ BLPOP with timeout
       ▼
┌─────────────────────────────────────────┐
│       Worker Service (Enhanced)         │
│  ┌────────────────────────────────┐    │
│  │ Metrics: Queue Wait, Engine    │    │
│  │ Circuit Breaker: Stockfish     │    │
│  │ Retry Logic: Connection        │    │
│  │ Graceful Shutdown Handler      │    │
│  └────────────────────────────────┘    │
│         /metrics (Prometheus)           │
└──────┬──────────────────────────────────┘
       │
       │ TCP Connection
       ▼
┌─────────────────────────────────────────┐
│      Stockfish Service (Monitored)      │
│  - Enhanced health checks               │
│  - Resource metrics exposed             │
└─────────────────────────────────────────┘

┌─────────────────────────────────────────┐
│          Prometheus Server              │
│  - Scrapes /metrics every 15s           │
│  - Stores time-series data              │
│  - Evaluates alerting rules             │
└──────┬──────────────────────────────────┘
       │
       ▼
┌─────────────────────────────────────────┐
│            Grafana Dashboards           │
│  - Latency percentiles                  │
│  - Auto-scaling metrics                 │
│  - Cost efficiency                      │
│  - Circuit breaker states               │
└─────────────────────────────────────────┘

┌─────────────────────────────────────────┐
│      KEDA + HPA (Enhanced Policies)     │
│  - Queue-based Worker scaling           │
│  - CPU-based Stockfish scaling          │
│  - Custom latency-based triggers        │
└─────────────────────────────────────────┘
```

## Components and Interfaces

### 1. Metrics Collection Library (Go)

A shared Go package for consistent metrics across services.

**Package:** `pkg/metrics`

**Key Types:**
```go
type MetricsCollector struct {
    requestDuration *prometheus.HistogramVec
    requestCounter  *prometheus.CounterVec
    queueDepth      prometheus.Gauge
    circuitState    *prometheus.GaugeVec
}

type LatencyTracker struct {
    startTime      time.Time  // Microsecond precision (requirement 1.1)
    correlationID  string
    checkpoints    map[string]time.Duration
}
```

**Exposed Metrics:**

**API Service Metrics (requirements 1.1, 1.5, 1.6, 1.8):**
- `api_request_duration_seconds` (histogram) - API request latency with percentiles (P50, P95, P99)
  - Buckets: [0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 2, 5, 10, 30]
  - Labels: endpoint, status_code
  - Calculated over 1m, 5m, 15m windows
- `api_requests_total` (counter) - Total requests by status code
- `api_successful_operations_total` (counter) - Completed jobs for cost tracking (requirement 5.1)

**Worker Service Metrics (requirements 1.2, 1.3, 1.4, 1.7):**
- `worker_queue_wait_seconds` (histogram) - Time jobs spend in queue (creation to dequeue)
- `worker_engine_connection_seconds` (histogram) - Stockfish connection time
- `worker_engine_computation_seconds` (histogram) - Stockfish computation time
- `worker_result_publish_seconds` (histogram) - Result publishing time
- `worker_total_processing_seconds` (histogram) - Total job processing time
- `worker_idle_time_seconds` (counter) - Time spent waiting for jobs (requirement 5.5)
- `worker_active_jobs` (gauge) - Current number of jobs being processed

**Circuit Breaker Metrics (requirement 3.8):**
- `circuit_breaker_state` (gauge) - 0=closed, 1=half-open, 2=open
  - Labels: service (redis/stockfish), component (api/worker)
- `circuit_breaker_failures_total` (counter) - Failure counts
  - Labels: service, component

**Retry Metrics (requirement 4.6):**
- `retry_attempts_total` (counter) - Retry counts by service and reason
  - Labels: service, operation, attempt_number

**Queue Metrics (requirement 5.7):**
- `redis_queue_depth` (gauge) - Current job queue size
- `redis_queue_depth_variance` (gauge) - Standard deviation over time windows

**Cost Efficiency Metrics (requirements 5.2, 5.3, 5.4):**
- `cost_efficiency_ratio` (gauge) - Operations per CPU-second
- `service_cpu_seconds_total` (counter) - Total CPU-seconds consumed
- `service_replica_count` (gauge) - Current replica count by service
- `service_average_replicas` (gauge) - Average replicas over 1-hour window

**Scaling Metrics (requirement 5.8):**
- `scaling_events_total` (counter) - Scale-up and scale-down events
  - Labels: service, direction (up/down)

**Design Rationale:** Histogram buckets are chosen to capture the full range of expected latencies from sub-millisecond to 30+ seconds. The metrics align directly with requirements for P50/P95/P99 calculations and cost tracking.

### 2. Circuit Breaker Implementation (Go)

**Package:** `pkg/circuitbreaker`

**Interface:**
```go
type CircuitBreaker interface {
    Call(func() error) error
    State() State
    Metrics() Metrics
}

type Config struct {
    FailureThreshold uint32
    SuccessThreshold uint32
    Timeout          time.Duration
    MaxRequests      uint32
}
```

**State Machine:**
```
Closed ──[failures >= threshold]──> Open
  ▲                                   │
  │                                   │ [timeout expires]
  │                                   ▼
  └──[success >= threshold]──── Half-Open
```

**Configuration by Use Case:**

1. **Worker → Stockfish Circuit Breaker (requirements 3.1-3.5):**
```go
StockfishCircuitBreaker: circuitbreaker.Config{
    FailureThreshold: 5,  // Open after 5 failures
    Timeout:          30 * time.Second, // within 60 seconds
    SuccessThreshold: 1,  // Close after 1 success in half-open
    MaxRequests:      1,  // Allow 1 test request in half-open
}
```

2. **API → Redis Circuit Breaker (requirements 3.6-3.7):**
```go
RedisCircuitBreaker: circuitbreaker.Config{
    FailureThreshold: 3,  // Open after 3 failures
    Timeout:          30 * time.Second, // within 30 seconds
    SuccessThreshold: 1,  // Close after 1 success
    MaxRequests:      1,  // Allow 1 test request in half-open
}
```

**Behavior Requirements:**
- When circuit opens, fail immediately without attempting connection (requirements 3.2, 3.7)
- After timeout expires, transition to half-open and allow one test request (requirement 3.3)
- If test succeeds, close circuit and resume normal operations (requirement 3.4)
- If test fails, reopen circuit for another timeout period (requirement 3.5)
- Expose state metrics: 0=closed, 1=half-open, 2=open (requirement 3.8)

**Design Rationale:** 
- Stockfish circuit breaker has higher threshold (5 failures) because individual engine instances may fail while others remain healthy
- Redis circuit breaker has lower threshold (3 failures) because Redis is a single point of failure
- 30-second timeout balances quick recovery with avoiding flapping
- Single test request in half-open prevents overwhelming recovering services

**Implementation Strategy:**
- Use `sony/gobreaker` library as foundation
- Wrap with custom metrics integration
- Separate circuit breakers for Redis and Stockfish connections
- Thread-safe state transitions

### 3. Retry Logic with Exponential Backoff (Go)

**Package:** `pkg/retry`

**Interface:**
```go
type RetryConfig struct {
    MaxAttempts     int
    InitialDelay    time.Duration
    MaxDelay        time.Duration
    Multiplier      float64
    JitterPercent   float64
}

func WithRetry(ctx context.Context, cfg RetryConfig, fn func() error) error
```

**Backoff Calculation:**
```
delay = min(initialDelay * (multiplier ^ attempt), maxDelay)
jitter = delay * (1 + random(-jitterPercent, +jitterPercent))
```

**Configuration by Use Case:**

1. **Worker → Stockfish Connections (requirements 4.1, 4.2, 4.5, 4.7):**
```go
StockfishRetry: retry.RetryConfig{
    MaxAttempts:   3,     // Maximum 3 retries
    InitialDelay:  100 * time.Millisecond,
    MaxDelay:      5 * time.Second,
    Multiplier:    2.0,   // Exponential backoff
    JitterPercent: 0.2,   // 20% jitter to prevent thundering herd
}
```

2. **API → Redis Job Publishing (requirement 4.3):**
```go
RedisPublishRetry: retry.RetryConfig{
    MaxAttempts:   2,     // Up to 2 retries
    InitialDelay:  50 * time.Millisecond,
    MaxDelay:      50 * time.Millisecond,
    Multiplier:    1.0,   // Fixed delay
    JitterPercent: 0.0,
}
```

3. **Worker → Redis Result Publishing (requirement 4.4):**
```go
RedisResultRetry: retry.RetryConfig{
    MaxAttempts:   3,     // Up to 3 retries
    InitialDelay:  100 * time.Millisecond,
    MaxDelay:      5 * time.Second,
    Multiplier:    2.0,   // Exponential backoff
    JitterPercent: 0.2,   // 20% jitter
}
```

**Design Rationale:** Different retry strategies are used based on the failure characteristics:
- Stockfish connections use exponential backoff because engine startup or network issues may take time to resolve
- Redis job publishing uses fixed delay because Redis failures are typically binary (up or down)
- Redis result publishing uses exponential backoff to handle transient load spikes
- All configurations respect requirement 4.6 for logging failures after exhausting retries

**Integration Points:**
- Worker → Stockfish TCP connections
- API → Redis job publishing
- Worker → Redis result publishing

### 4. Structured Logging (Go)

**Package:** `pkg/logging`

**Interface:**
```go
type Logger interface {
    WithCorrelationID(id string) Logger
    WithFields(fields map[string]interface{}) Logger
    Info(msg string)
    Error(msg string, err error)
    Warn(msg string)
}

type LogEntry struct {
    Timestamp     time.Time              `json:"timestamp"`
    Level         string                 `json:"level"`
    Message       string                 `json:"message"`
    CorrelationID string                 `json:"correlation_id,omitempty"`
    Service       string                 `json:"service"`
    Fields        map[string]interface{} `json:"fields,omitempty"`
}
```

**Implementation:**
- Use `sirupsen/logrus` with JSON formatter
- Inject correlation ID into context
- Extract correlation ID in middleware/handlers

### 5. Enhanced API Service

**New Endpoints:**
- `GET /metrics` - Prometheus metrics endpoint (port 8080)
- `GET /healthz` - Enhanced health check with dependency status

**Middleware Stack:**
```
Request → Correlation ID → Metrics → Circuit Breaker → Handler
```

**Request Flow with Instrumentation:**
1. Generate/extract correlation ID from X-Correlation-ID header
2. Start latency timer with microsecond precision (requirement 1.1)
3. Validate request (record validation time)
4. Check Redis circuit breaker state
5. Publish job to Redis with retry (record queue insertion timestamp per requirement 1.2)
6. Poll for results with timeout (record wait time)
7. Return response with X-Correlation-ID header (requirement 8.5)
8. Record total end-to-end latency (requirement 1.5)
9. Log structured entry with all timings

**Health Check Implementation:**
```go
type HealthStatus struct {
    Status         string `json:"status"` // "healthy" or "unhealthy"
    RedisConnected bool   `json:"redis_connected"`
    QueueDepth     int64  `json:"queue_depth"`
    Timestamp      string `json:"timestamp"`
}

func (a *API) HealthCheck() (HealthStatus, int) {
    // Check Redis connectivity with 2s timeout (requirement 6.1)
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()
    
    redisOk := a.redis.Ping(ctx).Err() == nil
    queueDepth := int64(0)
    
    if redisOk {
        queueDepth, _ = a.redis.LLen(ctx, "stockfish:jobs").Result()
    }
    
    status := "healthy"
    statusCode := http.StatusOK
    if !redisOk {
        status = "unhealthy"
        statusCode = http.StatusServiceUnavailable // requirement 6.4
    }
    
    return HealthStatus{
        Status:         status,
        RedisConnected: redisOk,
        QueueDepth:     queueDepth,
        Timestamp:      time.Now().Format(time.RFC3339),
    }, statusCode
}
```

**Design Rationale:** The health check returns detailed JSON status including queue depth to help operators diagnose issues. Returning 503 when Redis is unavailable allows load balancers to route traffic away from unhealthy instances.

**Circuit Breaker Configuration:**
```go
// Redis circuit breaker config (requirement 3.6)
RedisCircuitBreaker: circuitbreaker.Config{
    FailureThreshold: 3,  // Open after 3 failures
    Timeout:          30 * time.Second, // within 30 seconds
    SuccessThreshold: 1,  // Close after 1 success in half-open
    MaxRequests:      1,  // Allow 1 test request in half-open
}

// Retry config for Redis operations (requirement 4.3)
RedisRetry: retry.RetryConfig{
    MaxAttempts:   2,     // Up to 2 retries
    InitialDelay:  50 * time.Millisecond,
    MaxDelay:      50 * time.Millisecond, // Fixed delay
    Multiplier:    1.0,   // No exponential backoff for Redis
    JitterPercent: 0.0,
}
```

**Design Rationale:** Redis operations are typically fast, so we use a fixed 50ms delay rather than exponential backoff. The circuit breaker opens quickly (3 failures in 30s) to prevent cascading failures when Redis is down.

**New Configuration:**
```go
type Config struct {
    RedisAddr           string
    CircuitBreakerCfg   circuitbreaker.Config
    RetryCfg            retry.RetryConfig
    MetricsPort         int
    ShutdownTimeout     time.Duration // 30s per requirement 6.6
}
```

### 6. Enhanced Worker Service

**New Endpoints:**
- `GET /metrics` - Prometheus metrics endpoint (port 9090)
- `GET /healthz` - Health check with Redis and Stockfish status

**Processing Flow with Instrumentation:**
1. BLPOP job from Redis (blocking with timeout)
2. Extract correlation ID from job
3. Record queue wait time (current time - job creation time)
4. Check Stockfish circuit breaker
5. Connect to Stockfish with retry and backoff
6. Record connection time
7. Execute UCI commands and wait for bestmove
8. Record engine computation time
9. Publish result to Redis with retry
10. Record total processing time
11. Log structured entry with correlation ID

**Health Check Implementation:**
```go
type HealthStatus struct {
    Status           string `json:"status"` // "healthy" or "unhealthy"
    RedisConnected   bool   `json:"redis_connected"`
    StockfishHealthy bool   `json:"stockfish_healthy"`
    CurrentJobs      int    `json:"current_jobs"`
    Timestamp        string `json:"timestamp"`
}

func (w *Worker) HealthCheck() HealthStatus {
    // Check Redis with timeout (requirement 6.2)
    redisOk := w.checkRedis(2 * time.Second)
    
    // Check Stockfish with test connection (requirement 6.3)
    // Verify engine responsiveness within 2 seconds
    stockfishOk := w.checkStockfish(2 * time.Second)
    
    status := "healthy"
    if !redisOk || !stockfishOk {
        status = "unhealthy"
    }
    
    return HealthStatus{
        Status:           status,
        RedisConnected:   redisOk,
        StockfishHealthy: stockfishOk,
        CurrentJobs:      w.activeJobs,
        Timestamp:        time.Now().Format(time.RFC3339),
    }
}

// checkStockfish verifies engine responsiveness within 2 seconds
func (w *Worker) checkStockfish(timeout time.Duration) bool {
    ctx, cancel := context.WithTimeout(context.Background(), timeout)
    defer cancel()
    
    // Attempt to connect and send "uci" command
    conn, err := net.DialTimeout("tcp", w.stockfishAddr, timeout)
    if err != nil {
        return false
    }
    defer conn.Close()
    
    // Set read deadline
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
```

**Design Rationale:** The health check includes both dependency status and current workload to provide operators with actionable information. The 2-second timeout ensures health checks don't block for too long while still allowing for network latency.

**Graceful Shutdown:**
```go
func (w *Worker) Shutdown(ctx context.Context) error {
    // Stop accepting new jobs
    w.stopChan <- struct{}{}
    
    // Wait for current job to complete or timeout (max 30s per requirement 6.7)
    shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
    defer cancel()
    
    select {
    case <-w.doneChan:
        return nil
    case <-shutdownCtx.Done():
        return shutdownCtx.Err()
    }
}
```

**Design Rationale:** The 30-second timeout balances completing in-flight work with timely pod termination during deployments. Most chess calculations complete within 1-2 seconds, so 30 seconds provides ample buffer.

### 7. Prometheus Configuration

**Deployment:** Kubernetes StatefulSet with persistent volume

**Scrape Configuration:**
```yaml
scrape_configs:
  - job_name: 'api'
    kubernetes_sd_configs:
      - role: pod
        namespaces:
          names: ['stockfish']
    relabel_configs:
      - source_labels: [__meta_kubernetes_pod_label_app]
        regex: api
        action: keep
    scrape_interval: 15s
    
  - job_name: 'worker'
    kubernetes_sd_configs:
      - role: pod
        namespaces:
          names: ['stockfish']
    relabel_configs:
      - source_labels: [__meta_kubernetes_pod_label_app]
        regex: worker
        action: keep
    scrape_interval: 15s
    
  - job_name: 'redis'
    static_configs:
      - targets: ['redis-exporter:9121']
```

**Alerting Rules:**
```yaml
groups:
  - name: latency_alerts
    rules:
      - alert: HighAPILatency
        expr: histogram_quantile(0.95, api_request_duration_seconds) > 10
        for: 5m
        annotations:
          summary: "API P95 latency above 10s"
          
  - name: error_alerts
    rules:
      - alert: HighErrorRate
        expr: rate(api_requests_total{status=~"5.."}[2m]) > 0.05
        for: 2m
        annotations:
          summary: "Error rate above 5%"
```

### 8. Grafana Dashboards

**Dashboard 1: System Overview**
- Panels:
  - Request rate (requests/sec)
  - P50/P95/P99 latency graphs
  - Error rate percentage
  - Active replica counts per service
  - Redis queue depth with trend

**Dashboard 2: Auto-Scaling Metrics**
- Panels:
  - Worker replicas vs queue depth
  - Stockfish replicas vs CPU utilization
  - Scale-up/scale-down event timeline
  - Predicted vs actual scaling behavior

**Dashboard 3: Fault Tolerance**
- Panels:
  - Circuit breaker states (color-coded)
  - Retry attempt counts
  - Connection failure rates
  - Service dependency health matrix

**Dashboard 4: Cost Efficiency**
- Panels:
  - Operations per CPU-second
  - Estimated hourly cost
  - Idle time percentage per service
  - Resource utilization heatmap

### 9. Enhanced KEDA Configuration

**Queue-Based Worker Scaling (requirements 2.1, 2.4, 2.6):**
```yaml
apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata:
  name: worker-queue-scaler
spec:
  scaleTargetRef:
    name: worker
  minReplicaCount: 1
  maxReplicaCount: 20
  pollingInterval: 15
  cooldownPeriod: 300  # 5 minutes for scale-down
  triggers:
    - type: redis
      metadata:
        address: redis:6379
        listName: stockfish:jobs
        listLength: "10"  # 10 jobs per replica threshold
        activationListLength: "1"
  advanced:
    horizontalPodAutoscalerConfig:
      behavior:
        scaleUp:
          stabilizationWindowSeconds: 30
          policies:
            - type: Percent
              value: 100
              periodSeconds: 30
        scaleDown:
          stabilizationWindowSeconds: 300
```

**Design Rationale:** Queue depth is the primary scaling trigger because it directly reflects workload. The 10 jobs per replica threshold ensures workers aren't overwhelmed while maintaining efficiency. The 5-minute scale-down cooldown prevents flapping during variable load.

**Custom Metrics Scaler for Latency (requirement 2.3):**
```yaml
apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata:
  name: worker-latency-scaler
spec:
  scaleTargetRef:
    name: worker
  minReplicaCount: 1
  maxReplicaCount: 20
  pollingInterval: 15
  triggers:
    - type: prometheus
      metadata:
        serverAddress: http://prometheus:9090
        metricName: api_request_duration_p95
        threshold: '5'  # 5 seconds P95 threshold
        query: |
          histogram_quantile(0.95, 
            rate(api_request_duration_seconds_bucket[2m])
          )
```

**Design Rationale:** Latency-based scaling acts as a secondary quality-of-service trigger. When P95 latency exceeds 5 seconds for 2 consecutive minutes, it indicates the system is under stress even if queue depth is manageable (e.g., due to slow Stockfish instances).

**Combined Queue + Latency Scaling:**
- Primary trigger: Queue depth (fast response to workload changes)
- Secondary trigger: P95 latency (quality of service guarantee)
- Cooldown: 30s for scale-up, 300s for scale-down (requirement 2.4)

### 10. Enhanced HPA Configuration

**Multi-Metric HPA for Stockfish (requirements 2.2, 2.5, 2.7):**
```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: stockfish-multi-metric-hpa
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: stockfish
  minReplicas: 2  # Minimum for availability
  maxReplicas: 15
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 75  # Scale up when CPU > 75% for 60s
    - type: Resource
      resource:
        name: memory
        target:
          type: Utilization
          averageUtilization: 80
  behavior:
    scaleUp:
      stabilizationWindowSeconds: 60
      policies:
        - type: Percent
          value: 100  # Double replicas
          periodSeconds: 60
        - type: Pods
          value: 2    # Or add 2 pods
          periodSeconds: 60
      selectPolicy: Max  # Use whichever adds more pods
    scaleDown:
      stabilizationWindowSeconds: 600  # 10 minute cooldown
      policies:
        - type: Percent
          value: 25  # Remove 25% of replicas per 120s
          periodSeconds: 120
```

**Design Rationale:** 
- 75% CPU threshold provides headroom for traffic spikes while avoiding over-provisioning
- 60-second stabilization for scale-up ensures we respond to sustained load, not transient spikes
- 10-minute stabilization for scale-down prevents flapping and allows for traffic pattern observation
- Aggressive scale-up (100% or 2 pods) ensures quick response to load increases
- Conservative scale-down (25% per 2 minutes) prevents premature capacity reduction

**HPA for API Service (requirement 2.8):**
```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: api-hpa
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: api
  minReplicas: 2  # High availability requirement
  maxReplicas: 10
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 70
  behavior:
    scaleUp:
      stabilizationWindowSeconds: 60
      policies:
        - type: Percent
          value: 100
          periodSeconds: 60
    scaleDown:
      stabilizationWindowSeconds: 300
      policies:
        - type: Percent
          value: 50
          periodSeconds: 120
```

**Design Rationale:** API service maintains minimum 2 replicas for high availability. Lower CPU threshold (70%) ensures API remains responsive even under load. Faster scale-down (5 minutes) compared to Stockfish because API pods are lightweight and can be recreated quickly.

## Data Models

### Correlation ID Format
```
Format: {service}-{timestamp}-{random}
Example: api-1699564823-a3f9c2
```

### Job Payload (Enhanced)
```json
{
  "job_id": "job_1699564823_1600",
  "correlation_id": "api-1699564823-a3f9c2",
  "fen": "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1",
  "elo": 1600,
  "max_time_ms": 1000,
  "created_at": "2024-11-09T10:30:23.123456Z"
}
```

### Job Result (Enhanced)
```json
{
  "job_id": "job_1699564823_1600",
  "correlation_id": "api-1699564823-a3f9c2",
  "bestmove": "e2e4",
  "ponder": "e7e5",
  "info": "depth 20 score cp 25",
  "error": "",
  "timings": {
    "queue_wait_ms": 45,
    "engine_connect_ms": 12,
    "engine_compute_ms": 987,
    "total_ms": 1044
  },
  "completed_at": "2024-11-09T10:30:24.167890Z"
}
```

### Metrics Snapshot
```json
{
  "timestamp": "2024-11-09T10:30:00Z",
  "api": {
    "requests_per_sec": 45.2,
    "p50_latency_ms": 1234,
    "p95_latency_ms": 3456,
    "p99_latency_ms": 5678,
    "error_rate": 0.012,
    "replicas": 2
  },
  "worker": {
    "jobs_per_sec": 43.8,
    "queue_wait_p95_ms": 234,
    "engine_time_p95_ms": 1987,
    "replicas": 5,
    "idle_percentage": 15.3
  },
  "stockfish": {
    "cpu_utilization": 78.5,
    "memory_utilization": 62.3,
    "replicas": 6
  },
  "redis": {
    "queue_depth": 23,
    "operations_per_sec": 156.7
  },
  "cost": {
    "operations_per_cpu_second": 0.87,
    "estimated_hourly_cost_usd": 2.45
  }
}
```

## Error Handling

### Error Categories

1. **Transient Errors** (retry eligible):
   - Network timeouts
   - Connection refused
   - Redis temporary unavailability
   - Stockfish engine busy

2. **Permanent Errors** (no retry):
   - Invalid FEN format
   - Authentication failures
   - Resource limits exceeded

3. **Circuit Breaker Errors**:
   - Circuit open (fail fast)
   - Half-open test failure

### Error Response Format

**Circuit Breaker Open Response (requirement 3.7):**
```json
{
  "error": {
    "code": "SERVICE_UNAVAILABLE",
    "message": "Chess engine temporarily unavailable",
    "correlation_id": "api-1699564823-a3f9c2",
    "retry_after_seconds": 30,
    "details": {
      "circuit_breaker_state": "open",
      "failure_count": 5
    }
  }
}
```
HTTP Status: 503 Service Unavailable
Headers: `Retry-After: 30`

**Design Rationale:** Returning 503 with Retry-After header allows clients to implement intelligent retry logic. The correlation ID enables tracing the request through logs even when it fails at the circuit breaker level.

### Logging Error Context

```json
{
  "timestamp": "2024-11-09T10:30:23.456Z",
  "level": "error",
  "service": "worker",
  "correlation_id": "api-1699564823-a3f9c2",
  "message": "Failed to connect to Stockfish engine",
  "error": "dial tcp 10.0.0.5:4000: connection refused",
  "fields": {
    "job_id": "job_1699564823_1600",
    "engine_addr": "stockfish:4000",
    "attempt": 2,
    "max_attempts": 3,
    "backoff_ms": 200
  }
}
```

## Testing Strategy

### Unit Tests

1. **Metrics Collection**
   - Test histogram recording
   - Test counter increments
   - Test gauge updates
   - Verify Prometheus format output

2. **Circuit Breaker**
   - Test state transitions (closed → open → half-open → closed)
   - Test failure threshold triggering
   - Test timeout behavior
   - Test concurrent access safety

3. **Retry Logic**
   - Test exponential backoff calculation
   - Test jitter application
   - Test max attempts enforcement
   - Test context cancellation

4. **Correlation ID**
   - Test ID generation uniqueness
   - Test ID propagation through context
   - Test ID extraction from headers

### Integration Tests

1. **API Service**
   - Test metrics endpoint returns valid Prometheus format
   - Test correlation ID in response headers
   - Test circuit breaker opens after Redis failures
   - Test retry logic on transient Redis errors
   - Test graceful shutdown completes in-flight requests

2. **Worker Service**
   - Test job processing with full instrumentation
   - Test circuit breaker opens after Stockfish failures
   - Test retry logic on connection failures
   - Test graceful shutdown waits for job completion
   - Test metrics accuracy for queue wait time

3. **End-to-End**
   - Test correlation ID flows from API → Redis → Worker → Redis → API
   - Test latency measurements match actual processing time
   - Test auto-scaling triggers on queue depth
   - Test auto-scaling triggers on high latency
   - Test system behavior when circuit breakers open

### Load Tests

1. **Baseline Performance**
   - Measure overhead of metrics collection (< 1ms)
   - Measure overhead of correlation ID propagation (< 0.1ms)
   - Verify P99 latency under normal load

2. **Scaling Behavior**
   - Ramp up load from 10 to 100 req/s
   - Verify workers scale proportionally
   - Verify Stockfish scales based on CPU
   - Measure scale-up latency (< 60s)

3. **Fault Injection**
   - Kill Stockfish pods and verify circuit breaker opens
   - Introduce Redis latency and verify retries
   - Verify graceful degradation under partial failures

### Monitoring Tests

1. **Dashboard Validation**
   - Verify all panels display data
   - Verify alerts trigger correctly
   - Test dashboard refresh rates

2. **Metrics Accuracy**
   - Compare logged latencies with Prometheus metrics
   - Verify queue depth matches Redis LLEN
   - Verify replica counts match Kubernetes state

## Performance Considerations

### Latency Overhead

- Metrics collection: < 1ms per request
- Correlation ID generation: < 0.1ms
- Circuit breaker check: < 0.01ms
- Structured logging: < 0.5ms (async)
- **Total overhead: < 2ms** (negligible compared to engine computation time of 1000ms+)

### Memory Overhead

- Prometheus client library: ~5MB per service
- Circuit breaker state: < 1KB per breaker
- Correlation ID storage: ~50 bytes per request
- Log buffer: ~10MB per service

### Scaling Efficiency

- KEDA polling interval: 15s (balance between responsiveness and API load)
- HPA evaluation interval: 15s (Kubernetes default)
- Prometheus scrape interval: 15s (balance between granularity and storage)
- Metrics retention: 15 days (configurable based on storage)

### Cost Optimization Strategies

1. **Aggressive Scale-Down**: 5-minute cooldown for workers when queue is empty
2. **Minimum Replicas**: Keep API at 2 for HA, workers at 1 for cost savings
3. **Resource Requests**: Set accurate requests to avoid over-provisioning
4. **Spot Instances**: Use for worker and Stockfish pods (stateless, fault-tolerant)
5. **Metrics-Based Tuning**: Use cost efficiency ratio to identify waste

## Deployment Strategy

### Phase 1: Observability Foundation (Week 1)
- Deploy Prometheus and Grafana
- Add metrics endpoints to API and Worker
- Implement structured logging
- Create basic dashboards

### Phase 2: Fault Tolerance (Week 2)
- Implement circuit breakers
- Add retry logic with exponential backoff
- Enhance health checks
- Implement graceful shutdown

### Phase 3: Advanced Scaling (Week 3)
- Deploy enhanced KEDA configuration
- Deploy multi-metric HPA
- Add latency-based scaling triggers
- Tune scaling policies based on metrics

### Phase 4: Cost Optimization (Week 4)
- Implement cost tracking metrics
- Create cost efficiency dashboard
- Tune resource requests/limits
- Implement aggressive scale-down policies

### Rollback Plan

- All changes are backward compatible
- Metrics collection can be disabled via feature flag
- Circuit breakers can be configured to always-closed state
- Retry logic can be disabled by setting max attempts to 1
- Original scaling policies remain as fallback

## Security Considerations

- Metrics endpoints should be accessible only within cluster
- Grafana should require authentication
- Correlation IDs should not contain sensitive data
- Logs should not include PII or credentials
- Prometheus should use TLS for remote write (if applicable)
