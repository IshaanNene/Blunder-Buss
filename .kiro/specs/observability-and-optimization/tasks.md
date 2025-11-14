# Implementation Plan

- [x] 1. Create shared Go packages for observability infrastructure
  - Create `pkg/metrics` package with Prometheus collectors for latency histograms, counters, and gauges
  - Create `pkg/circuitbreaker` package wrapping sony/gobreaker with custom metrics integration
  - Create `pkg/retry` package implementing exponential backoff with jitter
  - Create `pkg/logging` package with structured JSON logging using logrus
  - Add correlation ID generation and context propagation utilities
  - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 1.7, 1.8, 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7, 3.8, 4.1, 4.2, 4.3, 4.4, 4.5, 4.6, 4.7, 8.1, 8.2, 8.3, 8.4, 8.5, 8.6, 8.7, 8.8_

- [x] 2. Enhance API service with observability and fault tolerance
  - [x] 2.1 Add Prometheus metrics endpoint and collectors
    - Implement `/metrics` endpoint on port 8080
    - Add request duration histogram with labels for status code and endpoint
    - Add request counter for total requests by status
    - Add Redis queue depth gauge updated on each job submission
    - Expose metrics in Prometheus format
    - _Requirements: 1.1, 1.5, 1.6, 1.8_
  
  - [x] 2.2 Implement correlation ID middleware
    - Create HTTP middleware to generate or extract correlation ID from X-Correlation-ID header
    - Store correlation ID in request context
    - Add correlation ID to all log entries
    - Include correlation ID in response headers
    - Add correlation ID to job payload sent to Redis
    - _Requirements: 8.1, 8.2, 8.5, 8.6_
  
  - [x] 2.3 Add circuit breaker for Redis connections
    - Initialize circuit breaker with config: 3 failures in 30s threshold, 30s timeout
    - Wrap Redis LPUSH operations with circuit breaker
    - Return HTTP 503 with retry-after header when circuit is open
    - Expose circuit breaker state and failure count metrics
    - _Requirements: 3.6, 3.7, 3.8_
  
  - [x] 2.4 Implement retry logic for Redis operations
    - Add retry wrapper for Redis job publishing with 2 max attempts
    - Use 50ms delay between retry attempts
    - Log retry attempts with correlation ID and attempt number
    - Increment retry metrics counter
    - _Requirements: 4.3, 4.6_
  
  - [x] 2.5 Add structured logging with timing information
    - Replace all log.Printf calls with structured logger
    - Log request start with correlation ID, method, path
    - Log request completion with correlation ID, status, duration
    - Log errors with full context including correlation ID and error details
    - Use JSON format for all log entries
    - _Requirements: 8.6, 8.7, 8.8_
  
  - [x] 2.6 Enhance health check endpoint
    - Update `/healthz` to return detailed JSON status
    - Check Redis connectivity with 2s timeout
    - Include Redis queue depth in health response
    - Return HTTP 503 if Redis is unavailable
    - _Requirements: 6.1, 6.4_
  
  - [x] 2.7 Implement graceful shutdown
    - Register signal handlers for SIGTERM and SIGINT
    - Stop accepting new requests on shutdown signal
    - Wait for in-flight requests to complete with 30s timeout
    - Close Redis connection cleanly
    - _Requirements: 6.6_

- [x] 3. Enhance Worker service with observability and fault tolerance
  - [x] 3.1 Add Prometheus metrics endpoint
    - Implement `/metrics` HTTP endpoint on port 9090
    - Add queue wait time histogram (job creation to dequeue)
    - Add engine connection time histogram
    - Add engine computation time histogram
    - Add total processing time histogram
    - Expose circuit breaker metrics for Stockfish connections
    - _Requirements: 1.2, 1.3, 1.4, 1.7, 1.8, 3.8_
  
  - [x] 3.2 Extract and propagate correlation IDs
    - Parse correlation ID from job payload
    - Store correlation ID in goroutine context
    - Include correlation ID in all log entries for job processing
    - Add correlation ID to result payload
    - _Requirements: 8.3, 8.4, 8.6_
  
  - [x] 3.3 Implement circuit breaker for Stockfish connections
    - Initialize circuit breaker with config: 5 failures in 60s threshold, 30s timeout
    - Wrap Stockfish TCP dial operations with circuit breaker
    - Fail jobs immediately when circuit is open with appropriate error message
    - Implement half-open state test connection logic
    - Expose circuit breaker state metrics
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.8_
  
  - [x] 3.4 Add retry logic with exponential backoff for Stockfish connections
    - Implement retry wrapper for TCP dial with 3 max attempts
    - Use exponential backoff starting at 100ms with 2.0 multiplier
    - Cap maximum backoff at 5 seconds
    - Add 20% jitter to backoff delays
    - Log each retry attempt with backoff duration
    - _Requirements: 4.1, 4.2, 4.5, 4.7_
  
  - [x] 3.5 Add retry logic for Redis result publishing
    - Wrap Redis RPUSH with retry logic: 3 max attempts
    - Use exponential backoff with jitter
    - Log failures after all retries exhausted
    - _Requirements: 4.4, 4.6_
  
  - [x] 3.6 Add timing instrumentation to job processing
    - Record job creation timestamp from payload
    - Calculate and record queue wait time on dequeue
    - Record Stockfish connection start and duration
    - Record engine computation start and duration
    - Record result publishing duration
    - Add all timing data to result payload
    - Log structured entry with all timings on job completion
    - _Requirements: 1.2, 1.3, 1.4, 8.8_
  
  - [x] 3.7 Implement health check endpoint
    - Create HTTP server on port 9090 for health and metrics
    - Implement `/healthz` endpoint returning JSON status
    - Check Redis connectivity with 2s timeout
    - Check Stockfish connectivity with test connection (verify engine responsiveness within 2 seconds)
    - Include current job count in health response
    - _Requirements: 6.2, 6.3, 6.5_
  
  - [x] 3.8 Implement graceful shutdown
    - Create shutdown channel and done channel
    - Stop BLPOP loop on shutdown signal
    - Wait for current job to complete with 30s timeout
    - Close Redis and Stockfish connections cleanly
    - _Requirements: 6.7_

- [x] 4. Deploy Prometheus for metrics collection
  - Create Prometheus StatefulSet with persistent volume in k8s/prometheus-deployment.yaml
  - Create Prometheus ConfigMap with scrape configs for api, worker, and redis-exporter jobs
  - Configure 15s scrape interval for all targets
  - Set up Kubernetes service discovery for dynamic pod discovery
  - Create Prometheus Service exposing port 9090
  - Add alerting rules for high latency (P95 > 10s for 5m) and high error rate (> 5% for 2m)
  - _Requirements: 1.8, 7.7, 7.8_

- [x] 5. Deploy Redis Exporter for queue metrics
  - Create Redis Exporter Deployment in k8s/redis-exporter-deployment.yaml
  - Configure exporter to connect to Redis service
  - Expose metrics on port 9121
  - Create Service for Prometheus to scrape
  - _Requirements: 5.7_

- [x] 6. Deploy Grafana with dashboards
  - [x] 6.1 Create Grafana deployment and service
    - Create Grafana Deployment in k8s/grafana-deployment.yaml
    - Configure Prometheus as data source
    - Create ConfigMap for dashboard provisioning
    - Expose Grafana on NodePort 30300
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.5, 7.6_
  
  - [x] 6.2 Create System Overview dashboard
    - Add panel for request rate (rate of api_requests_total)
    - Add panel for P50/P95/P99 latency using histogram_quantile
    - Add panel for error rate percentage
    - Add panel for active replica counts per service
    - Add panel for Redis queue depth with trend line
    - _Requirements: 7.1, 7.2, 7.3_
  
  - [x] 6.3 Create Auto-Scaling Metrics dashboard
    - Add panel for worker replicas vs queue depth correlation
    - Add panel for Stockfish replicas vs CPU utilization
    - Add panel for scale-up/scale-down event timeline
    - Add panel showing scaling policy thresholds
    - _Requirements: 7.2_
  
  - [x] 6.4 Create Fault Tolerance dashboard
    - Add panel for circuit breaker states with color coding (green=closed, yellow=half-open, red=open)
    - Add panel for retry attempt counts by service
    - Add panel for connection failure rates
    - Add service dependency health matrix
    - _Requirements: 7.4, 7.6_
  
  - [x] 6.5 Create Cost Efficiency dashboard
    - Add panel for operations per CPU-second ratio
    - Add panel for estimated hourly cost calculation
    - Add panel for idle time percentage per service
    - Add resource utilization heatmap
    - _Requirements: 7.5_

- [x] 7. Enhance Kubernetes auto-scaling configurations
  - [x] 7.1 Update KEDA ScaledObject for queue-based worker scaling
    - Update k8s/keda-scaledobject-queue.yaml with refined thresholds
    - Set queue depth threshold to 10 jobs per replica
    - Configure 30s cooldown for scale-up, 300s for scale-down
    - Set minReplicaCount to 1, maxReplicaCount to 20
    - _Requirements: 2.1, 2.4, 2.6_
  
  - [x] 7.2 Create KEDA ScaledObject for latency-based worker scaling
    - Create k8s/keda-scaledobject-latency.yaml with Prometheus trigger
    - Query P95 API latency from Prometheus
    - Set threshold to 5 seconds
    - Configure as secondary scaling trigger
    - _Requirements: 2.3_
  
  - [x] 7.3 Update HPA for multi-metric Stockfish scaling
    - Update k8s/hpa-stockfish.yaml with CPU and memory metrics
    - Set CPU threshold to 75%, memory threshold to 80%
    - Configure minReplicas to 2, maxReplicas to 15
    - Add scale-up policy: 100% or 2 pods per 60s (max)
    - Add scale-down policy: 25% per 120s with 600s stabilization
    - _Requirements: 2.2, 2.5, 2.7_
  
  - [x] 7.4 Create HPA for API service scaling
    - Create k8s/hpa-api.yaml with CPU-based scaling
    - Set minReplicas to 2 for high availability
    - Set maxReplicas to 10
    - Configure 70% CPU threshold
    - _Requirements: 2.8_

- [x] 8. Add cost and efficiency tracking
  - [x] 8.1 Implement cost metrics in API service
    - Add counter for successful operations (completed jobs)
    - Calculate operations per second metric
    - Expose metric for Prometheus scraping
    - _Requirements: 5.1_
  
  - [x] 8.2 Implement resource tracking in Worker service
    - Track CPU-seconds consumed using container metrics
    - Calculate cost efficiency ratio (operations / CPU-seconds)
    - Expose efficiency metrics
    - _Requirements: 5.2, 5.3_
  
  - [x] 8.3 Add replica count tracking
    - Query Kubernetes API for current replica counts
    - Calculate average replicas over 1-hour windows
    - Expose as Prometheus metrics
    - _Requirements: 5.4_
  
  - [x] 8.4 Implement idle time tracking for workers
    - Track time spent waiting for jobs vs processing
    - Calculate idle percentage
    - Expose idle time metrics
    - _Requirements: 5.5_
  
  - [x] 8.5 Add queue depth variance tracking
    - Calculate standard deviation of queue depth over time windows
    - Expose variance metrics for provisioning analysis
    - _Requirements: 5.7_
  
  - [x] 8.6 Track scaling event ratios
    - Count scale-up and scale-down events
    - Calculate ratio for tuning analysis
    - Expose as metrics
    - _Requirements: 5.8_

- [x] 9. Update deployment configurations
  - Update go/api/go.mod to include prometheus client, logrus, gobreaker, and retry dependencies
  - Update go/worker/go.mod with same dependencies
  - Update docker/api/Dockerfile to copy pkg directory and build with new dependencies
  - Update docker/worker/Dockerfile to copy pkg directory and build with new dependencies
  - Update k8s/api-deployment.yaml to expose metrics port 8080 and add resource requests/limits
  - Update k8s/worker-deployment.yaml to expose metrics port 9090 and add resource requests/limits
  - _Requirements: All_

- [x] 10. Create documentation and runbooks
  - Create docs/observability-guide.md explaining metrics, dashboards, and alerting
  - Create docs/troubleshooting-runbook.md with common issues and resolution steps
  - Create docs/cost-optimization-guide.md with tuning recommendations
  - Update README.md with links to new documentation and architecture diagrams
  - _Requirements: All_
