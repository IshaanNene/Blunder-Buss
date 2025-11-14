# Observability Guide

## Overview

This guide explains the comprehensive observability infrastructure implemented in the Blunder-Buss chess platform. The system provides detailed metrics, structured logging, distributed tracing via correlation IDs, and real-time dashboards for monitoring system health, performance, and cost efficiency.

## Architecture

```
┌─────────────┐
│   Client    │
└──────┬──────┘
       │ HTTP + X-Correlation-ID
       ▼
┌─────────────────────────────────────────┐
│         API Service (Enhanced)          │
│  - Metrics: Latency, Throughput         │
│  - Circuit Breaker: Redis               │
│  - Structured Logging + Correlation ID  │
│  - /metrics endpoint (port 8080)        │
└──────┬──────────────────────┬───────────┘
       │                      │
       ▼                      ▼
┌─────────────────────────────────────────┐
│         Redis Queue (Monitored)         │
└──────┬──────────────────────────────────┘
       │
       ▼
┌─────────────────────────────────────────┐
│       Worker Service (Enhanced)         │
│  - Metrics: Queue Wait, Engine Time     │
│  - Circuit Breaker: Stockfish           │
│  - /metrics endpoint (port 9090)        │
└──────┬──────────────────────────────────┘
       │
       ▼
┌─────────────────────────────────────────┐
│      Stockfish Service (Monitored)      │
└─────────────────────────────────────────┘

         ┌──────────────┐
         │  Prometheus  │ ← Scrapes metrics every 15s
         └──────┬───────┘
                │
                ▼
         ┌──────────────┐
         │   Grafana    │ ← Visualizes metrics
         └──────────────┘
```

## Metrics Collection

### API Service Metrics

**Endpoint:** `http://api:8080/metrics`

#### Request Latency
- **Metric:** `api_request_duration_seconds` (histogram)
- **Description:** End-to-end request latency from receipt to response
- **Labels:** `endpoint`, `status_code`
- **Buckets:** 0.001s, 0.005s, 0.01s, 0.05s, 0.1s, 0.5s, 1s, 2s, 5s, 10s, 30s
- **Usage:** Calculate P50, P95, P99 latencies over time windows

**Example PromQL queries:**
```promql
# P95 latency over last 5 minutes
histogram_quantile(0.95, rate(api_request_duration_seconds_bucket[5m]))

# P99 latency by endpoint
histogram_quantile(0.99, rate(api_request_duration_seconds_bucket{endpoint="/analyze"}[5m]))
```

#### Request Counts
- **Metric:** `api_requests_total` (counter)
- **Description:** Total number of requests by status code
- **Labels:** `status_code`, `endpoint`

**Example PromQL queries:**
```promql
# Request rate (requests per second)
rate(api_requests_total[1m])

# Error rate percentage
rate(api_requests_total{status_code=~"5.."}[5m]) / rate(api_requests_total[5m]) * 100
```

#### Successful Operations
- **Metric:** `api_successful_operations_total` (counter)
- **Description:** Count of successfully completed chess jobs
- **Usage:** Cost efficiency calculations

#### Circuit Breaker State
- **Metric:** `circuit_breaker_state` (gauge)
- **Description:** Circuit breaker state (0=closed, 1=half-open, 2=open)
- **Labels:** `service` (redis), `component` (api)

### Worker Service Metrics

**Endpoint:** `http://worker:9090/metrics`

#### Queue Wait Time
- **Metric:** `worker_queue_wait_seconds` (histogram)
- **Description:** Time jobs spend in Redis queue (creation to dequeue)
- **Usage:** Identify queueing bottlenecks

**Example PromQL queries:**
```promql
# P95 queue wait time
histogram_quantile(0.95, rate(worker_queue_wait_seconds_bucket[5m]))
```

#### Engine Connection Time
- **Metric:** `worker_engine_connection_seconds` (histogram)
- **Description:** Time to establish TCP connection to Stockfish

#### Engine Computation Time
- **Metric:** `worker_engine_computation_seconds` (histogram)
- **Description:** Time Stockfish spends calculating best move

#### Total Processing Time
- **Metric:** `worker_total_processing_seconds` (histogram)
- **Description:** End-to-end job processing time

#### Idle Time
- **Metric:** `worker_idle_time_seconds` (counter)
- **Description:** Cumulative time workers spend waiting for jobs
- **Usage:** Identify over-provisioning

**Example PromQL queries:**
```promql
# Idle time percentage
rate(worker_idle_time_seconds[5m]) / (rate(worker_idle_time_seconds[5m]) + rate(worker_total_processing_seconds_count[5m])) * 100
```

#### Active Jobs
- **Metric:** `worker_active_jobs` (gauge)
- **Description:** Current number of jobs being processed

### Redis Queue Metrics

**Endpoint:** `http://redis-exporter:9121/metrics`

#### Queue Depth
- **Metric:** `redis_queue_depth` (gauge)
- **Description:** Current number of jobs in stockfish:jobs queue
- **Usage:** Primary trigger for worker auto-scaling

#### Queue Depth Variance
- **Metric:** `redis_queue_depth_variance` (gauge)
- **Description:** Standard deviation of queue depth over time windows
- **Usage:** Identify provisioning patterns

### Circuit Breaker Metrics

Available on both API and Worker metrics endpoints.

- **Metric:** `circuit_breaker_state` (gauge)
  - Values: 0=closed (healthy), 1=half-open (testing), 2=open (failing)
  - Labels: `service` (redis/stockfish), `component` (api/worker)

- **Metric:** `circuit_breaker_failures_total` (counter)
  - Labels: `service`, `component`

### Retry Metrics

- **Metric:** `retry_attempts_total` (counter)
- **Description:** Count of retry attempts by service and operation
- **Labels:** `service`, `operation`, `attempt_number`

### Cost Efficiency Metrics

#### Operations per CPU-Second
- **Metric:** `cost_efficiency_ratio` (gauge)
- **Description:** Successful operations divided by CPU-seconds consumed
- **Usage:** Optimize resource allocation

#### CPU Consumption
- **Metric:** `service_cpu_seconds_total` (counter)
- **Description:** Total CPU-seconds consumed by service
- **Labels:** `service`

#### Replica Counts
- **Metric:** `service_replica_count` (gauge)
- **Description:** Current number of replicas
- **Labels:** `service`

- **Metric:** `service_average_replicas` (gauge)
- **Description:** Average replicas over 1-hour window
- **Labels:** `service`

#### Scaling Events
- **Metric:** `scaling_events_total` (counter)
- **Description:** Count of scale-up and scale-down events
- **Labels:** `service`, `direction` (up/down)

## Structured Logging

### Log Format

All services use structured JSON logging with the following format:

```json
{
  "timestamp": "2024-11-09T10:30:23.456Z",
  "level": "info",
  "service": "api",
  "correlation_id": "api-1699564823-a3f9c2",
  "message": "Request completed",
  "fields": {
    "method": "POST",
    "path": "/analyze",
    "status": 200,
    "duration_ms": 1234,
    "queue_depth": 15
  }
}
```

### Correlation IDs

**Format:** `{service}-{timestamp}-{random}`  
**Example:** `api-1699564823-a3f9c2`

Correlation IDs flow through the entire request lifecycle:
1. Generated or extracted by API service from `X-Correlation-ID` header
2. Included in job payload sent to Redis
3. Extracted by Worker service and included in all logs
4. Included in result payload
5. Returned to client in `X-Correlation-ID` response header

**Usage:** Search logs by correlation ID to trace a request across all services.

### Log Levels

- **INFO:** Normal operations (request start/complete, job processing)
- **WARN:** Recoverable issues (retry attempts, circuit breaker half-open)
- **ERROR:** Failures requiring attention (exhausted retries, circuit breaker open)

### Example Log Queries

**Find all logs for a specific request:**
```bash
kubectl logs -l app=api -n stockfish | grep "api-1699564823-a3f9c2"
kubectl logs -l app=worker -n stockfish | grep "api-1699564823-a3f9c2"
```

**Find all errors in the last hour:**
```bash
kubectl logs -l app=api -n stockfish --since=1h | grep '"level":"error"'
```

## Grafana Dashboards

Access Grafana at: `http://<node-ip>:30300`

### Dashboard 1: System Overview

**Purpose:** High-level system health and performance

**Panels:**
- **Request Rate:** Requests per second over time
- **Latency Percentiles:** P50, P95, P99 latency graphs
- **Error Rate:** Percentage of 5xx responses
- **Active Replicas:** Current replica counts for API, Worker, Stockfish
- **Queue Depth:** Redis queue depth with trend line

**Key Metrics:**
- Normal P95 latency: < 5 seconds
- Normal error rate: < 1%
- Queue depth should correlate with worker replicas

### Dashboard 2: Auto-Scaling Metrics

**Purpose:** Monitor and tune auto-scaling behavior

**Panels:**
- **Worker Replicas vs Queue Depth:** Correlation between queue size and worker count
- **Stockfish Replicas vs CPU:** CPU utilization and replica count
- **Scaling Events Timeline:** Visual timeline of scale-up/down events
- **Scaling Thresholds:** Current metrics vs configured thresholds

**Key Metrics:**
- Worker scaling threshold: 10 jobs per replica
- Stockfish CPU threshold: 75%
- Scale-up cooldown: 30s
- Scale-down cooldown: 300s (workers), 600s (Stockfish)

### Dashboard 3: Fault Tolerance

**Purpose:** Monitor circuit breakers and retry behavior

**Panels:**
- **Circuit Breaker States:** Color-coded states (green=closed, yellow=half-open, red=open)
- **Retry Attempts:** Count of retry attempts by service
- **Connection Failures:** Failure rates for Redis and Stockfish connections
- **Service Health Matrix:** Dependency health status

**Key Metrics:**
- Circuit breaker should be closed (0) under normal conditions
- Retry attempts should be minimal (< 5% of requests)
- Connection failures should be < 1%

### Dashboard 4: Cost Efficiency

**Purpose:** Optimize resource allocation and reduce costs

**Panels:**
- **Operations per CPU-Second:** Efficiency ratio over time
- **Estimated Hourly Cost:** Calculated based on replica counts and resource requests
- **Idle Time Percentage:** Worker idle time vs processing time
- **Resource Utilization Heatmap:** CPU/memory usage across pods

**Key Metrics:**
- Target idle time: < 20%
- Target operations per CPU-second: > 0.5
- Identify over-provisioned services for cost reduction

## Alerting

### Alert Rules

Alerts are configured in Prometheus and displayed in Grafana.

#### High API Latency
- **Condition:** P95 latency > 10 seconds for 5 minutes
- **Severity:** Warning
- **Action:** Check worker and Stockfish scaling, investigate slow jobs

#### High Error Rate
- **Condition:** Error rate > 5% for 2 minutes
- **Severity:** Critical
- **Action:** Check service health, circuit breaker states, logs for errors

#### Circuit Breaker Open
- **Condition:** Circuit breaker state = 2 (open)
- **Severity:** Critical
- **Action:** Investigate dependency health (Redis or Stockfish)

#### Queue Depth High
- **Condition:** Queue depth > 100 for 5 minutes
- **Severity:** Warning
- **Action:** Verify worker scaling is functioning, check Stockfish capacity

#### Low Cost Efficiency
- **Condition:** Operations per CPU-second < 0.3 for 1 hour
- **Severity:** Info
- **Action:** Review resource requests, consider reducing replicas

### Alert Notification Channels

Configure notification channels in Grafana:
- Slack
- PagerDuty
- Email
- Webhook

## Prometheus Configuration

### Scrape Targets

Prometheus scrapes metrics from:
- API pods: `http://api:8080/metrics` (every 15s)
- Worker pods: `http://worker:9090/metrics` (every 15s)
- Redis exporter: `http://redis-exporter:9121/metrics` (every 15s)

### Data Retention

- Default: 15 days
- Configurable via `--storage.tsdb.retention.time` flag
- Adjust based on storage capacity and query needs

### Query Performance

- Use recording rules for frequently queried metrics
- Limit query time ranges for complex queries
- Use `rate()` instead of `irate()` for smoother graphs

## Best Practices

### Monitoring

1. **Check dashboards daily** for anomalies
2. **Set up alerts** for critical metrics
3. **Review cost efficiency** weekly to optimize resources
4. **Correlate metrics** across services to identify bottlenecks

### Logging

1. **Always include correlation IDs** when investigating issues
2. **Use structured logging** for easy parsing and filtering
3. **Set appropriate log levels** to avoid noise
4. **Aggregate logs** in a centralized system (e.g., ELK stack)

### Metrics

1. **Use histograms** for latency measurements (not averages)
2. **Label metrics consistently** across services
3. **Avoid high-cardinality labels** (e.g., user IDs, correlation IDs)
4. **Monitor metric cardinality** to prevent Prometheus overload

### Troubleshooting

1. **Start with dashboards** to identify affected components
2. **Check circuit breaker states** for dependency issues
3. **Search logs by correlation ID** to trace requests
4. **Compare metrics** before and after incidents

## Performance Overhead

The observability infrastructure adds minimal overhead:
- Metrics collection: < 1ms per request
- Correlation ID generation: < 0.1ms
- Structured logging: < 0.5ms (async)
- **Total overhead: < 2ms** (negligible compared to 1000ms+ engine computation)

## Security Considerations

- Metrics endpoints are accessible only within the Kubernetes cluster
- Grafana requires authentication (configure in deployment)
- Correlation IDs do not contain sensitive data
- Logs do not include PII or credentials
- Use TLS for Prometheus remote write if applicable

## Further Reading

- [Prometheus Documentation](https://prometheus.io/docs/)
- [Grafana Documentation](https://grafana.com/docs/)
- [PromQL Cheat Sheet](https://promlabs.com/promql-cheat-sheet/)
- [Troubleshooting Runbook](./troubleshooting-runbook.md)
- [Cost Optimization Guide](./cost-optimization-guide.md)
