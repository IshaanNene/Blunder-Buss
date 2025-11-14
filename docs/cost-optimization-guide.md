# Cost Optimization Guide

## Overview

This guide provides strategies and recommendations for optimizing infrastructure costs while maintaining performance and reliability in the Blunder-Buss chess platform. The system includes comprehensive cost tracking metrics and dashboards to identify optimization opportunities.

## Cost Tracking Metrics

### Operations per CPU-Second

**Metric:** `cost_efficiency_ratio`

This is the primary cost efficiency metric, calculated as:
```
cost_efficiency_ratio = successful_operations / cpu_seconds_consumed
```

**Target:** > 0.5 operations per CPU-second

**PromQL Query:**
```promql
rate(api_successful_operations_total[1h]) / rate(service_cpu_seconds_total[1h])
```

**Interpretation:**
- **> 0.8:** Excellent efficiency
- **0.5 - 0.8:** Good efficiency
- **0.3 - 0.5:** Room for improvement
- **< 0.3:** Significant waste, immediate action needed

### Idle Time Percentage

**Metric:** `worker_idle_time_seconds`

Tracks time workers spend waiting for jobs vs. processing.

**Target:** < 20% idle time

**PromQL Query:**
```promql
rate(worker_idle_time_seconds[5m]) / 
(rate(worker_idle_time_seconds[5m]) + rate(worker_total_processing_seconds_count[5m])) * 100
```

**Interpretation:**
- **< 10%:** Optimal utilization
- **10-20%:** Acceptable
- **20-40%:** Consider reducing min replicas
- **> 40%:** Significant over-provisioning

### Average Replica Count

**Metric:** `service_average_replicas`

Tracks average replicas over 1-hour windows.

**Usage:** Identify services that maintain high replica counts unnecessarily.

**PromQL Query:**
```promql
avg_over_time(service_replica_count[1h])
```

### Estimated Hourly Cost

Calculate estimated cost based on replica counts and resource requests.

**Formula:**
```
hourly_cost = Σ(replicas × cpu_request × cpu_cost_per_hour + replicas × memory_request × memory_cost_per_hour)
```

**Example Calculation:**
```
# Assuming AWS pricing (adjust for your cloud provider)
CPU cost: $0.04 per vCPU-hour
Memory cost: $0.005 per GB-hour

API Service (2 replicas, 0.5 CPU, 256MB each):
  CPU: 2 × 0.5 × $0.04 = $0.04/hour
  Memory: 2 × 0.25 × $0.005 = $0.0025/hour
  Total: $0.0425/hour

Worker Service (5 replicas, 0.5 CPU, 512MB each):
  CPU: 5 × 0.5 × $0.04 = $0.10/hour
  Memory: 5 × 0.5 × $0.005 = $0.0125/hour
  Total: $0.1125/hour

Stockfish Service (6 replicas, 1 CPU, 1GB each):
  CPU: 6 × 1 × $0.04 = $0.24/hour
  Memory: 6 × 1 × $0.005 = $0.03/hour
  Total: $0.27/hour

Total System Cost: ~$0.43/hour = ~$310/month
```

## Optimization Strategies

### 1. Right-Size Resource Requests and Limits

**Problem:** Over-provisioned resource requests waste money and prevent efficient bin-packing.

**Diagnosis:**
```bash
# Check actual resource usage
kubectl top pods -n stockfish

# Compare with resource requests
kubectl get deployment -n stockfish api -o yaml | grep -A 5 resources
```

**Optimization Steps:**

1. **Monitor actual usage over 1 week:**
   ```promql
   # CPU usage
   max_over_time(container_cpu_usage_seconds_total[7d])
   
   # Memory usage
   max_over_time(container_memory_working_set_bytes[7d])
   ```

2. **Set requests to P95 usage + 20% buffer:**
   ```yaml
   resources:
     requests:
       cpu: "400m"      # If P95 is 330m
       memory: "384Mi"  # If P95 is 320Mi
     limits:
       cpu: "1000m"     # 2-3x requests for burst capacity
       memory: "512Mi"  # 1.5x requests to prevent OOM
   ```

3. **Apply changes and monitor:**
   ```bash
   kubectl apply -f k8s/api-deployment.yaml
   kubectl rollout status deployment -n stockfish api
   ```

**Expected Savings:** 20-40% reduction in resource costs

### 2. Optimize Auto-Scaling Policies

**Problem:** Slow scale-down or high minimum replicas keep unnecessary pods running.

#### Worker Service Optimization

**Current Configuration:**
```yaml
minReplicaCount: 1
maxReplicaCount: 20
cooldownPeriod: 300  # 5 minutes
```

**Optimization:**

1. **Reduce minimum replicas to 0 during off-peak hours:**
   ```yaml
   minReplicaCount: 0  # Scale to zero when queue is empty
   activationListLength: "1"  # Wake up when first job arrives
   ```

2. **Implement time-based scaling for predictable patterns:**
   ```yaml
   # Use KEDA cron scaler for business hours
   triggers:
     - type: cron
       metadata:
         timezone: America/New_York
         start: 0 9 * * 1-5    # 9 AM weekdays
         end: 0 18 * * 1-5     # 6 PM weekdays
         desiredReplicas: "3"
   ```

3. **Aggressive scale-down for cost savings:**
   ```yaml
   cooldownPeriod: 120  # Reduce from 300s to 120s
   ```

**Expected Savings:** 30-50% on worker costs during off-peak hours

#### Stockfish Service Optimization

**Current Configuration:**
```yaml
minReplicas: 2
maxReplicas: 15
```

**Optimization:**

1. **Reduce minimum replicas if acceptable:**
   ```yaml
   minReplicas: 1  # If single point of failure is acceptable
   ```

2. **Tune CPU threshold for faster scale-down:**
   ```yaml
   metrics:
     - type: Resource
       resource:
         name: cpu
         target:
           type: Utilization
           averageUtilization: 70  # Reduce from 75 for earlier scale-down
   ```

3. **Faster scale-down policy:**
   ```yaml
   behavior:
     scaleDown:
       stabilizationWindowSeconds: 300  # Reduce from 600s
       policies:
         - type: Percent
           value: 50  # More aggressive: remove 50% per 120s
           periodSeconds: 120
   ```

**Expected Savings:** 15-25% on Stockfish costs

### 3. Use Spot/Preemptible Instances

**Problem:** On-demand instances are expensive for stateless, fault-tolerant workloads.

**Applicable Services:**
- Worker Service (stateless, fault-tolerant with retries)
- Stockfish Service (stateless, replaceable)

**Not Applicable:**
- API Service (needs high availability)
- Redis (stateful, needs persistence)

**Implementation:**

1. **Create node pool with spot instances:**
   ```bash
   # AWS EKS example
   eksctl create nodegroup \
     --cluster=blunder-buss \
     --name=spot-workers \
     --spot \
     --instance-types=t3.medium,t3a.medium \
     --nodes-min=0 \
     --nodes-max=20
   ```

2. **Add node affinity to deployments:**
   ```yaml
   # k8s/worker-deployment.yaml
   spec:
     template:
       spec:
         affinity:
           nodeAffinity:
             preferredDuringSchedulingIgnoredDuringExecution:
               - weight: 100
                 preference:
                   matchExpressions:
                     - key: node.kubernetes.io/instance-type
                       operator: In
                       values:
                         - spot
   ```

3. **Add tolerations for spot instance taints:**
   ```yaml
   tolerations:
     - key: "node.kubernetes.io/spot"
       operator: "Exists"
       effect: "NoSchedule"
   ```

**Expected Savings:** 60-70% on worker and Stockfish compute costs

### 4. Implement Queue Depth-Based Throttling

**Problem:** Unlimited job acceptance can cause queue buildup and unnecessary scaling.

**Implementation:**

1. **Add queue depth check in API:**
   ```go
   const maxQueueDepth = 200
   
   func (a *API) HandleAnalyze(w http.ResponseWriter, r *http.Request) {
       queueDepth, _ := a.redis.LLen(ctx, "stockfish:jobs").Result()
       
       if queueDepth > maxQueueDepth {
           http.Error(w, "System at capacity, please retry later", http.StatusTooManyRequests)
           w.Header().Set("Retry-After", "30")
           return
       }
       
       // Process request...
   }
   ```

2. **Add rate limiting per client:**
   ```go
   // Use token bucket or sliding window rate limiter
   limiter := rate.NewLimiter(rate.Limit(10), 20)  // 10 req/s, burst 20
   ```

**Expected Savings:** 10-20% by preventing unnecessary scale-ups

### 5. Optimize Redis Configuration

**Problem:** Over-provisioned Redis wastes resources.

**Current Configuration:**
```yaml
resources:
  requests:
    cpu: "500m"
    memory: "512Mi"
  limits:
    cpu: "1000m"
    memory: "1Gi"
```

**Optimization:**

1. **Monitor Redis memory usage:**
   ```bash
   kubectl exec -n stockfish redis-0 -- redis-cli INFO memory
   ```

2. **Set maxmemory policy:**
   ```bash
   kubectl exec -n stockfish redis-0 -- redis-cli CONFIG SET maxmemory 256mb
   kubectl exec -n stockfish redis-0 -- redis-cli CONFIG SET maxmemory-policy allkeys-lru
   ```

3. **Reduce resource requests if usage is low:**
   ```yaml
   resources:
     requests:
       cpu: "250m"      # Reduce if CPU usage < 50%
       memory: "256Mi"  # Reduce if memory usage < 50%
   ```

**Expected Savings:** 30-50% on Redis costs

### 6. Implement Caching

**Problem:** Repeated analysis of same positions wastes compute.

**Implementation:**

1. **Add Redis cache for FEN positions:**
   ```go
   func (a *API) GetCachedResult(fen string, elo int) (*Result, bool) {
       key := fmt.Sprintf("cache:%s:%d", fen, elo)
       result, err := a.redis.Get(ctx, key).Result()
       if err == nil {
           return parseResult(result), true
       }
       return nil, false
   }
   
   func (a *API) CacheResult(fen string, elo int, result *Result) {
       key := fmt.Sprintf("cache:%s:%d", fen, elo)
       a.redis.Set(ctx, key, result, 1*time.Hour)  // 1 hour TTL
   }
   ```

2. **Monitor cache hit rate:**
   ```go
   cacheHits := prometheus.NewCounter(...)
   cacheMisses := prometheus.NewCounter(...)
   ```

**Expected Savings:** 20-40% reduction in compute for repeated positions

### 7. Schedule Maintenance During Off-Peak Hours

**Problem:** Deployments and maintenance during peak hours waste resources.

**Best Practices:**

1. **Identify off-peak hours:**
   ```promql
   # Analyze request rate by hour
   avg_over_time(rate(api_requests_total[1h])[7d:1h])
   ```

2. **Schedule deployments during low-traffic periods:**
   ```bash
   # Example: Deploy at 2 AM
   kubectl set image deployment/api api=new-image:v2.0 -n stockfish
   ```

3. **Use PodDisruptionBudgets to prevent disruption:**
   ```yaml
   apiVersion: policy/v1
   kind: PodDisruptionBudget
   metadata:
     name: api-pdb
   spec:
     minAvailable: 1
     selector:
       matchLabels:
         app: api
   ```

### 8. Optimize Prometheus Retention

**Problem:** Long retention periods consume expensive storage.

**Current Configuration:**
```yaml
args:
  - '--storage.tsdb.retention.time=15d'
```

**Optimization:**

1. **Reduce retention for high-cardinality metrics:**
   ```yaml
   args:
     - '--storage.tsdb.retention.time=7d'  # Reduce from 15d
   ```

2. **Use remote write for long-term storage:**
   ```yaml
   remote_write:
     - url: "https://prometheus-remote-storage.example.com/api/v1/write"
       queue_config:
         capacity: 10000
         max_samples_per_send: 5000
   ```

3. **Implement metric relabeling to drop unnecessary labels:**
   ```yaml
   metric_relabel_configs:
     - source_labels: [__name__]
       regex: 'go_.*'  # Drop Go runtime metrics
       action: drop
   ```

**Expected Savings:** 40-60% on storage costs

## Cost Monitoring Dashboard

### Key Panels

1. **Estimated Hourly Cost**
   - Formula: `sum(service_replica_count * resource_requests * cloud_pricing)`
   - Alert if cost > budget threshold

2. **Cost Efficiency Ratio**
   - Formula: `rate(api_successful_operations_total[1h]) / rate(service_cpu_seconds_total[1h])`
   - Alert if ratio < 0.3

3. **Idle Time by Service**
   - Formula: `rate(worker_idle_time_seconds[5m]) / (rate(worker_idle_time_seconds[5m]) + rate(worker_total_processing_seconds_count[5m]))`
   - Alert if idle time > 40%

4. **Resource Utilization Heatmap**
   - Shows CPU/memory usage across all pods
   - Identify under-utilized pods

5. **Scaling Event Ratio**
   - Formula: `scaling_events_total{direction="up"} / scaling_events_total{direction="down"}`
   - Optimal ratio: 1.0-1.5 (slightly more scale-ups than scale-downs)

## Optimization Workflow

### Weekly Review

1. **Check Cost Efficiency Dashboard**
   - Review estimated hourly cost trend
   - Identify services with low efficiency ratio
   - Check for high idle time

2. **Analyze Resource Usage**
   ```bash
   # Get average resource usage over past week
   kubectl top pods -n stockfish --sort-by=cpu
   kubectl top pods -n stockfish --sort-by=memory
   ```

3. **Review Scaling Events**
   ```promql
   # Count scaling events by service
   sum by (service) (increase(scaling_events_total[7d]))
   ```

4. **Identify Optimization Opportunities**
   - Services with idle time > 30%
   - Services with efficiency ratio < 0.4
   - Services with resource usage < 50% of requests

### Monthly Optimization Sprint

1. **Implement 2-3 optimization strategies** from this guide
2. **Measure impact** over 1 week
3. **Document savings** and update baselines
4. **Share results** with team

## Cost Optimization Checklist

- [ ] Resource requests match actual usage (P95 + 20% buffer)
- [ ] Resource limits set to 2-3x requests for CPU, 1.5x for memory
- [ ] Worker min replicas = 0 or 1 (not higher)
- [ ] Stockfish min replicas = 1 or 2 (not higher)
- [ ] Scale-down cooldown < 5 minutes for workers
- [ ] Spot instances used for workers and Stockfish
- [ ] Queue depth throttling implemented (max 200 jobs)
- [ ] Redis memory < 256MB (right-sized)
- [ ] Caching implemented for repeated positions
- [ ] Prometheus retention = 7 days (not longer)
- [ ] Idle time < 20% for all services
- [ ] Cost efficiency ratio > 0.5
- [ ] Deployments scheduled during off-peak hours
- [ ] Cost monitoring dashboard reviewed weekly

## Expected Total Savings

By implementing all optimization strategies:

| Strategy | Savings |
|----------|---------|
| Right-size resources | 20-40% |
| Optimize auto-scaling | 30-50% |
| Use spot instances | 60-70% (on applicable services) |
| Implement caching | 20-40% (compute reduction) |
| Optimize Redis | 30-50% |
| Reduce Prometheus retention | 40-60% (storage) |

**Combined Expected Savings:** 40-60% of total infrastructure costs

**Example:**
- Current cost: $310/month
- Optimized cost: $125-185/month
- **Savings: $125-185/month ($1,500-2,200/year)**

## Monitoring Cost Optimization Impact

### Before Optimization Baseline

Record baseline metrics:
```promql
# Average hourly cost
avg_over_time(estimated_hourly_cost[7d])

# Average efficiency ratio
avg_over_time(cost_efficiency_ratio[7d])

# Average idle time
avg_over_time(worker_idle_time_percentage[7d])
```

### After Optimization Measurement

Compare metrics after 1 week:
```promql
# Cost reduction percentage
(baseline_cost - current_cost) / baseline_cost * 100

# Efficiency improvement
(current_efficiency - baseline_efficiency) / baseline_efficiency * 100
```

### Success Criteria

- Cost reduced by at least 30%
- Efficiency ratio improved by at least 20%
- P95 latency not increased by more than 10%
- Error rate not increased

## Advanced Optimization Techniques

### 1. Predictive Scaling

Use historical data to predict load and pre-scale:

```yaml
# KEDA cron scaler for predictable patterns
triggers:
  - type: cron
    metadata:
      timezone: America/New_York
      start: 0 8 * * 1-5     # Pre-scale at 8 AM weekdays
      end: 0 19 * * 1-5      # Scale down at 7 PM
      desiredReplicas: "5"
```

### 2. Multi-Region Deployment

Deploy to cheaper regions for non-latency-sensitive workloads:

- US East (Virginia): Baseline pricing
- US West (Oregon): ~5% cheaper
- Asia Pacific (Mumbai): ~10% more expensive

### 3. Reserved Instances / Savings Plans

For stable baseline capacity:
- Reserve capacity for minimum replicas (API: 2, Stockfish: 2)
- Use spot instances for burst capacity
- Expected savings: 30-40% on reserved capacity

### 4. Cluster Autoscaling

Enable cluster autoscaler to remove unused nodes:

```yaml
# Cluster autoscaler configuration
--scale-down-enabled=true
--scale-down-delay-after-add=5m
--scale-down-unneeded-time=5m
--scale-down-utilization-threshold=0.5
```

## Troubleshooting Cost Issues

### Cost Suddenly Increased

1. **Check for scaling events:**
   ```promql
   increase(scaling_events_total[1h])
   ```

2. **Check for stuck replicas:**
   ```bash
   kubectl get hpa -n stockfish
   kubectl get scaledobject -n stockfish
   ```

3. **Check for resource limit increases:**
   ```bash
   kubectl get deployment -n stockfish -o yaml | grep -A 5 resources
   ```

### Efficiency Ratio Decreased

1. **Check for increased idle time:**
   ```promql
   rate(worker_idle_time_seconds[5m])
   ```

2. **Check for decreased throughput:**
   ```promql
   rate(api_successful_operations_total[5m])
   ```

3. **Check for increased resource consumption:**
   ```promql
   rate(service_cpu_seconds_total[5m])
   ```

## Related Documentation

- [Observability Guide](./observability-guide.md) - Metrics and monitoring
- [Troubleshooting Runbook](./troubleshooting-runbook.md) - Issue resolution
- [Architecture Documentation](../README.md) - System overview
