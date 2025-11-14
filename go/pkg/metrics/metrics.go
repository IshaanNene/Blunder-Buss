package metrics

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// MetricsCollector provides Prometheus metrics collection for services
type MetricsCollector struct {
	// API Service metrics
	requestDuration *prometheus.HistogramVec
	requestCounter  *prometheus.CounterVec
	queueDepth      prometheus.Gauge
	
	// Worker Service metrics
	queueWaitTime        *prometheus.HistogramVec
	engineConnectionTime *prometheus.HistogramVec
	engineComputeTime    *prometheus.HistogramVec
	resultPublishTime    *prometheus.HistogramVec
	totalProcessingTime  *prometheus.HistogramVec
	idleTime             prometheus.Counter
	idlePercentage       prometheus.Gauge
	activeJobs           prometheus.Gauge
	
	// Circuit breaker metrics
	circuitState   *prometheus.GaugeVec
	circuitFailures *prometheus.CounterVec
	
	// Retry metrics
	retryAttempts *prometheus.CounterVec
	
	// Cost efficiency metrics
	successfulOps      prometheus.Counter
	cpuSeconds         prometheus.Counter
	costEfficiency     prometheus.Gauge
	replicaCount       *prometheus.GaugeVec
	averageReplicas    *prometheus.GaugeVec
	
	// Queue metrics
	queueDepthVariance prometheus.Gauge
	
	// Scaling metrics
	scalingEvents      *prometheus.CounterVec
	scalingEventsRatio *prometheus.GaugeVec
}

// NewMetricsCollector creates a new metrics collector for the specified service
func NewMetricsCollector(serviceName string) *MetricsCollector {
	mc := &MetricsCollector{}
	
	// API Service metrics (requirements 1.1, 1.5, 1.6, 1.8)
	if serviceName == "api" {
		mc.requestDuration = promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "api_request_duration_seconds",
				Help:    "API request latency with percentiles (P50, P95, P99)",
				Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 2, 5, 10, 30},
			},
			[]string{"endpoint", "status_code"},
		)
		
		mc.requestCounter = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "api_requests_total",
				Help: "Total requests by status code",
			},
			[]string{"status_code"},
		)
		
		mc.queueDepth = promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "redis_queue_depth",
				Help: "Current job queue size",
			},
		)
		
		mc.successfulOps = promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "api_successful_operations_total",
				Help: "Completed jobs for cost tracking",
			},
		)
	}
	
	// Worker Service metrics (requirements 1.2, 1.3, 1.4, 1.7)
	if serviceName == "worker" {
		mc.queueWaitTime = promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "worker_queue_wait_seconds",
				Help:    "Time jobs spend in queue (creation to dequeue)",
				Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 2, 5, 10, 30, 60},
			},
			[]string{},
		)
		
		mc.engineConnectionTime = promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "worker_engine_connection_seconds",
				Help:    "Stockfish connection time",
				Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 2, 5},
			},
			[]string{},
		)
		
		mc.engineComputeTime = promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "worker_engine_computation_seconds",
				Help:    "Stockfish computation time",
				Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60},
			},
			[]string{},
		)
		
		mc.resultPublishTime = promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "worker_result_publish_seconds",
				Help:    "Result publishing time",
				Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1},
			},
			[]string{},
		)
		
		mc.totalProcessingTime = promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "worker_total_processing_seconds",
				Help:    "Total job processing time",
				Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60, 120},
			},
			[]string{},
		)
		
		mc.idleTime = promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "worker_idle_time_seconds",
				Help: "Time spent waiting for jobs",
			},
		)
		
		mc.idlePercentage = promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "worker_idle_percentage",
				Help: "Idle time percentage (0-100)",
			},
		)
		
		mc.activeJobs = promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "worker_active_jobs",
				Help: "Current number of jobs being processed",
			},
		)
	}
	
	// Circuit breaker metrics (requirement 3.8)
	mc.circuitState = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "circuit_breaker_state",
			Help: "Circuit breaker state: 0=closed, 1=half-open, 2=open",
		},
		[]string{"service", "component"},
	)
	
	mc.circuitFailures = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "circuit_breaker_failures_total",
			Help: "Circuit breaker failure counts",
		},
		[]string{"service", "component"},
	)
	
	// Retry metrics (requirement 4.6)
	mc.retryAttempts = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "retry_attempts_total",
			Help: "Retry counts by service and reason",
		},
		[]string{"service", "operation", "attempt_number"},
	)
	
	// Cost efficiency metrics (requirements 5.1, 5.2, 5.3, 5.4)
	mc.cpuSeconds = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "service_cpu_seconds_total",
			Help: "Total CPU-seconds consumed",
		},
	)
	
	mc.costEfficiency = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "cost_efficiency_ratio",
			Help: "Operations per CPU-second",
		},
	)
	
	mc.replicaCount = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "service_replica_count",
			Help: "Current replica count by service",
		},
		[]string{"service"},
	)
	
	mc.averageReplicas = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "service_average_replicas",
			Help: "Average replicas over 1-hour window",
		},
		[]string{"service"},
	)
	
	// Queue metrics (requirement 5.7)
	mc.queueDepthVariance = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "redis_queue_depth_variance",
			Help: "Standard deviation of queue depth over time windows",
		},
	)
	
	// Scaling metrics (requirement 5.8)
	mc.scalingEvents = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "scaling_events_total",
			Help: "Scale-up and scale-down events",
		},
		[]string{"service", "direction"},
	)
	
	mc.scalingEventsRatio = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "scaling_events_ratio",
			Help: "Ratio of scale-up events to scale-down events for tuning analysis",
		},
		[]string{"service"},
	)
	
	return mc
}

// RecordRequestDuration records API request duration
func (mc *MetricsCollector) RecordRequestDuration(endpoint, statusCode string, duration time.Duration) {
	if mc.requestDuration != nil {
		mc.requestDuration.WithLabelValues(endpoint, statusCode).Observe(duration.Seconds())
	}
}

// IncrementRequestCounter increments API request counter
func (mc *MetricsCollector) IncrementRequestCounter(statusCode string) {
	if mc.requestCounter != nil {
		mc.requestCounter.WithLabelValues(statusCode).Inc()
	}
}

// SetQueueDepth sets the current queue depth
func (mc *MetricsCollector) SetQueueDepth(depth float64) {
	if mc.queueDepth != nil {
		mc.queueDepth.Set(depth)
	}
}

// RecordQueueWaitTime records worker queue wait time
func (mc *MetricsCollector) RecordQueueWaitTime(duration time.Duration) {
	if mc.queueWaitTime != nil {
		mc.queueWaitTime.WithLabelValues().Observe(duration.Seconds())
	}
}

// RecordEngineConnectionTime records Stockfish connection time
func (mc *MetricsCollector) RecordEngineConnectionTime(duration time.Duration) {
	if mc.engineConnectionTime != nil {
		mc.engineConnectionTime.WithLabelValues().Observe(duration.Seconds())
	}
}

// RecordEngineComputeTime records Stockfish computation time
func (mc *MetricsCollector) RecordEngineComputeTime(duration time.Duration) {
	if mc.engineComputeTime != nil {
		mc.engineComputeTime.WithLabelValues().Observe(duration.Seconds())
	}
}

// RecordResultPublishTime records result publishing time
func (mc *MetricsCollector) RecordResultPublishTime(duration time.Duration) {
	if mc.resultPublishTime != nil {
		mc.resultPublishTime.WithLabelValues().Observe(duration.Seconds())
	}
}

// RecordTotalProcessingTime records total job processing time
func (mc *MetricsCollector) RecordTotalProcessingTime(duration time.Duration) {
	if mc.totalProcessingTime != nil {
		mc.totalProcessingTime.WithLabelValues().Observe(duration.Seconds())
	}
}

// IncrementIdleTime increments worker idle time
func (mc *MetricsCollector) IncrementIdleTime(duration time.Duration) {
	if mc.idleTime != nil {
		mc.idleTime.Add(duration.Seconds())
	}
}

// SetIdlePercentage sets the worker idle percentage (0-100)
func (mc *MetricsCollector) SetIdlePercentage(percentage float64) {
	if mc.idlePercentage != nil {
		mc.idlePercentage.Set(percentage)
	}
}

// SetActiveJobs sets the current number of active jobs
func (mc *MetricsCollector) SetActiveJobs(count float64) {
	if mc.activeJobs != nil {
		mc.activeJobs.Set(count)
	}
}

// SetCircuitBreakerState sets circuit breaker state (0=closed, 1=half-open, 2=open)
func (mc *MetricsCollector) SetCircuitBreakerState(service, component string, state float64) {
	if mc.circuitState != nil {
		mc.circuitState.WithLabelValues(service, component).Set(state)
	}
}

// IncrementCircuitBreakerFailures increments circuit breaker failure count
func (mc *MetricsCollector) IncrementCircuitBreakerFailures(service, component string) {
	if mc.circuitFailures != nil {
		mc.circuitFailures.WithLabelValues(service, component).Inc()
	}
}

// IncrementRetryAttempts increments retry attempt counter
func (mc *MetricsCollector) IncrementRetryAttempts(service, operation, attemptNumber string) {
	if mc.retryAttempts != nil {
		mc.retryAttempts.WithLabelValues(service, operation, attemptNumber).Inc()
	}
}

// IncrementSuccessfulOps increments successful operations counter
func (mc *MetricsCollector) IncrementSuccessfulOps() {
	if mc.successfulOps != nil {
		mc.successfulOps.Inc()
	}
}

// IncrementCPUSeconds increments CPU seconds counter
func (mc *MetricsCollector) IncrementCPUSeconds(seconds float64) {
	if mc.cpuSeconds != nil {
		mc.cpuSeconds.Add(seconds)
	}
}

// SetCostEfficiency sets the cost efficiency ratio
func (mc *MetricsCollector) SetCostEfficiency(ratio float64) {
	if mc.costEfficiency != nil {
		mc.costEfficiency.Set(ratio)
	}
}

// SetReplicaCount sets the current replica count
func (mc *MetricsCollector) SetReplicaCount(service string, count float64) {
	if mc.replicaCount != nil {
		mc.replicaCount.WithLabelValues(service).Set(count)
	}
}

// SetAverageReplicas sets the average replica count
func (mc *MetricsCollector) SetAverageReplicas(service string, avg float64) {
	if mc.averageReplicas != nil {
		mc.averageReplicas.WithLabelValues(service).Set(avg)
	}
}

// SetQueueDepthVariance sets the queue depth variance
func (mc *MetricsCollector) SetQueueDepthVariance(variance float64) {
	if mc.queueDepthVariance != nil {
		mc.queueDepthVariance.Set(variance)
	}
}

// IncrementScalingEvents increments scaling event counter
func (mc *MetricsCollector) IncrementScalingEvents(service, direction string) {
	if mc.scalingEvents != nil {
		mc.scalingEvents.WithLabelValues(service, direction).Inc()
	}
}

// SetScalingEventsRatio sets the ratio of scale-up to scale-down events
func (mc *MetricsCollector) SetScalingEventsRatio(service string, ratio float64) {
	if mc.scalingEventsRatio != nil {
		mc.scalingEventsRatio.WithLabelValues(service).Set(ratio)
	}
}

// LatencyTracker tracks latency with microsecond precision (requirement 1.1)
type LatencyTracker struct {
	startTime     time.Time
	correlationID string
	checkpoints   map[string]time.Duration
	mu            sync.RWMutex
}

// NewLatencyTracker creates a new latency tracker
func NewLatencyTracker(correlationID string) *LatencyTracker {
	return &LatencyTracker{
		startTime:     time.Now(),
		correlationID: correlationID,
		checkpoints:   make(map[string]time.Duration),
	}
}

// Checkpoint records a timing checkpoint
func (lt *LatencyTracker) Checkpoint(name string) {
	lt.mu.Lock()
	defer lt.mu.Unlock()
	lt.checkpoints[name] = time.Since(lt.startTime)
}

// GetDuration returns the duration since start
func (lt *LatencyTracker) GetDuration() time.Duration {
	return time.Since(lt.startTime)
}

// GetCheckpoint returns the duration at a specific checkpoint
func (lt *LatencyTracker) GetCheckpoint(name string) (time.Duration, bool) {
	lt.mu.RLock()
	defer lt.mu.RUnlock()
	duration, ok := lt.checkpoints[name]
	return duration, ok
}

// GetCorrelationID returns the correlation ID
func (lt *LatencyTracker) GetCorrelationID() string {
	return lt.correlationID
}

// GetAllCheckpoints returns all checkpoints
func (lt *LatencyTracker) GetAllCheckpoints() map[string]time.Duration {
	lt.mu.RLock()
	defer lt.mu.RUnlock()
	
	result := make(map[string]time.Duration, len(lt.checkpoints))
	for k, v := range lt.checkpoints {
		result[k] = v
	}
	return result
}
