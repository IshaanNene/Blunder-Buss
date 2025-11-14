# Troubleshooting Runbook

## Overview

This runbook provides step-by-step procedures for diagnosing and resolving common issues in the Blunder-Buss chess platform. Each section includes symptoms, diagnosis steps, and resolution procedures.

## Quick Reference

| Symptom | Likely Cause | Section |
|---------|--------------|---------|
| High latency (P95 > 10s) | Queue backlog or slow engines | [High Latency](#high-latency) |
| 503 errors from API | Redis circuit breaker open | [API Service Unavailable](#api-service-unavailable-503) |
| Jobs not processing | Worker-Stockfish connection issues | [Jobs Stuck in Queue](#jobs-stuck-in-queue) |
| Workers not scaling | KEDA misconfiguration | [Auto-Scaling Not Working](#auto-scaling-not-working) |
| Circuit breaker stuck open | Dependency down or misconfigured | [Circuit Breaker Stuck Open](#circuit-breaker-stuck-open) |
| High error rate | Multiple possible causes | [High Error Rate](#high-error-rate) |
| Pods crashing | Resource limits or bugs | [Pod Crashes](#pod-crashes-crashloopbackoff) |

## High Latency

### Symptoms
- P95 latency > 10 seconds
- Grafana alert: "High API Latency"
- Slow response times reported by users

### Diagnosis

1. **Check System Overview Dashboard**
   ```
   Navigate to Grafana → System Overview Dashboard
   ```
   - Look at latency percentiles graph
   - Check queue depth
   - Verify replica counts

2. **Identify Bottleneck**
   ```promql
   # Check queue wait time
   histogram_quantile(0.95, rate(worker_queue_wait_seconds_bucket[5m]))
   
   # Check engine computation time
   histogram_quantile(0.95, rate(worker_engine_computation_seconds_bucket[5m]))
   ```

3. **Check Worker Scaling**
   ```bash
   kubectl get pods -n stockfish -l app=worker
   kubectl get scaledobject -n stockfish worker-queue-scaler -o yaml
   ```

4. **Check Stockfish Scaling**
   ```bash
   kubectl get pods -n stockfish -l app=stockfish
   kubectl get hpa -n stockfish stockfish-multi-metric-hpa
   ```

### Resolution

**If queue wait time is high (> 1s):**
1. Verify KEDA is scaling workers:
   ```bash
   kubectl describe scaledobject -n stockfish worker-queue-scaler
   ```
2. Check if max replicas reached:
   ```bash
   kubectl get scaledobject -n stockfish worker-queue-scaler -o jsonpath='{.spec.maxReplicaCount}'
   ```
3. If at max, increase `maxReplicaCount` in `k8s/keda-scaledobject-queue.yaml`
4. Apply changes:
   ```bash
   kubectl apply -f k8s/keda-scaledobject-queue.yaml
   ```

**If engine computation time is high (> 5s):**
1. Check Stockfish CPU utilization:
   ```bash
   kubectl top pods -n stockfish -l app=stockfish
   ```
2. Verify HPA is scaling:
   ```bash
   kubectl describe hpa -n stockfish stockfish-multi-metric-hpa
   ```
3. If CPU > 75% and not scaling, check HPA configuration
4. Consider increasing Stockfish resource limits

**If neither is high:**
1. Check API service logs for errors:
   ```bash
   kubectl logs -n stockfish -l app=api --tail=100
   ```
2. Check for network issues between services
3. Verify Redis performance:
   ```bash
   kubectl exec -n stockfish redis-0 -- redis-cli INFO stats
   ```

### Prevention
- Set up alerts for queue depth > 50
- Monitor scaling events to ensure auto-scaling is responsive
- Review cost efficiency to ensure adequate provisioning

---

## API Service Unavailable (503)

### Symptoms
- API returns HTTP 503 "Service Unavailable"
- Response includes `Retry-After` header
- Grafana shows circuit breaker state = 2 (open)

### Diagnosis

1. **Check Circuit Breaker State**
   ```promql
   circuit_breaker_state{service="redis", component="api"}
   ```
   - 0 = closed (healthy)
   - 1 = half-open (testing)
   - 2 = open (failing)

2. **Check Redis Health**
   ```bash
   kubectl get pods -n stockfish -l app=redis
   kubectl logs -n stockfish redis-0 --tail=50
   ```

3. **Test Redis Connectivity**
   ```bash
   kubectl exec -n stockfish -it redis-0 -- redis-cli PING
   ```

4. **Check API Logs**
   ```bash
   kubectl logs -n stockfish -l app=api --tail=100 | grep -i "circuit\|redis"
   ```

### Resolution

**If Redis pod is down:**
1. Check pod status:
   ```bash
   kubectl describe pod -n stockfish redis-0
   ```
2. Check for resource issues:
   ```bash
   kubectl top pod -n stockfish redis-0
   ```
3. Restart Redis if necessary:
   ```bash
   kubectl delete pod -n stockfish redis-0
   ```
   (StatefulSet will recreate it)

**If Redis is healthy but circuit breaker is open:**
1. Wait 30 seconds for circuit breaker to transition to half-open
2. Monitor circuit breaker state:
   ```promql
   circuit_breaker_state{service="redis", component="api"}
   ```
3. If it closes (state = 0), issue is resolved
4. If it reopens, investigate Redis performance:
   ```bash
   kubectl exec -n stockfish redis-0 -- redis-cli --latency
   ```

**If Redis is overloaded:**
1. Check queue depth:
   ```bash
   kubectl exec -n stockfish redis-0 -- redis-cli LLEN stockfish:jobs
   ```
2. Scale workers to drain queue faster
3. Consider increasing Redis resources

### Prevention
- Monitor Redis health endpoint
- Set up alerts for circuit breaker state changes
- Ensure Redis has adequate resources
- Consider Redis clustering for high availability

---

## Jobs Stuck in Queue

### Symptoms
- Queue depth increasing continuously
- Jobs not being processed
- Worker pods running but idle

### Diagnosis

1. **Check Queue Depth**
   ```bash
   kubectl exec -n stockfish redis-0 -- redis-cli LLEN stockfish:jobs
   ```

2. **Check Worker Status**
   ```bash
   kubectl get pods -n stockfish -l app=worker
   kubectl logs -n stockfish -l app=worker --tail=50
   ```

3. **Check Worker Circuit Breaker**
   ```promql
   circuit_breaker_state{service="stockfish", component="worker"}
   ```

4. **Check Stockfish Health**
   ```bash
   kubectl get pods -n stockfish -l app=stockfish
   kubectl logs -n stockfish -l app=stockfish --tail=50
   ```

### Resolution

**If worker circuit breaker is open:**
1. Check Stockfish pod health:
   ```bash
   kubectl get pods -n stockfish -l app=stockfish
   ```
2. Test Stockfish connectivity:
   ```bash
   kubectl exec -n stockfish -it <worker-pod> -- nc -zv stockfish 4000
   ```
3. Restart unhealthy Stockfish pods:
   ```bash
   kubectl delete pod -n stockfish <stockfish-pod>
   ```
4. Wait for circuit breaker to close (30s timeout)

**If workers are not running:**
1. Check KEDA scaler:
   ```bash
   kubectl get scaledobject -n stockfish worker-queue-scaler
   kubectl describe scaledobject -n stockfish worker-queue-scaler
   ```
2. Verify KEDA operator is running:
   ```bash
   kubectl get pods -n keda
   ```
3. Check KEDA logs:
   ```bash
   kubectl logs -n keda -l app=keda-operator
   ```

**If workers are running but not processing:**
1. Check worker logs for errors:
   ```bash
   kubectl logs -n stockfish -l app=worker --tail=100
   ```
2. Verify Redis connectivity from worker:
   ```bash
   kubectl exec -n stockfish -it <worker-pod> -- nc -zv redis 6379
   ```
3. Check for deadlocks or stuck goroutines (restart workers):
   ```bash
   kubectl rollout restart deployment -n stockfish worker
   ```

### Prevention
- Monitor queue depth with alerts (> 100 for 5 minutes)
- Ensure KEDA is properly configured
- Set up health checks for all services
- Monitor circuit breaker states

---

## Auto-Scaling Not Working

### Symptoms
- Queue depth high but workers not scaling
- CPU high but Stockfish not scaling
- Replicas stuck at min or max

### Diagnosis

1. **Check KEDA Status**
   ```bash
   kubectl get scaledobject -n stockfish
   kubectl describe scaledobject -n stockfish worker-queue-scaler
   ```

2. **Check HPA Status**
   ```bash
   kubectl get hpa -n stockfish
   kubectl describe hpa -n stockfish stockfish-multi-metric-hpa
   ```

3. **Check Metrics Server**
   ```bash
   kubectl get apiservice v1beta1.metrics.k8s.io -o yaml
   kubectl top nodes
   ```

4. **Check KEDA Operator Logs**
   ```bash
   kubectl logs -n keda -l app=keda-operator --tail=100
   ```

### Resolution

**If KEDA scaler shows errors:**
1. Check Redis connectivity from KEDA:
   ```bash
   kubectl exec -n keda -it <keda-operator-pod> -- nc -zv redis.stockfish 6379
   ```
2. Verify scaler configuration:
   ```bash
   kubectl get scaledobject -n stockfish worker-queue-scaler -o yaml
   ```
3. Check for authentication issues (if Redis requires auth)
4. Restart KEDA operator:
   ```bash
   kubectl rollout restart deployment -n keda keda-operator
   ```

**If HPA shows "unknown" metrics:**
1. Verify metrics-server is running:
   ```bash
   kubectl get deployment -n kube-system metrics-server
   ```
2. Check metrics-server logs:
   ```bash
   kubectl logs -n kube-system -l k8s-app=metrics-server
   ```
3. Verify resource requests are set on pods:
   ```bash
   kubectl get deployment -n stockfish stockfish -o yaml | grep -A 5 resources
   ```

**If at max replicas:**
1. Check current replica count:
   ```bash
   kubectl get deployment -n stockfish worker -o jsonpath='{.spec.replicas}'
   ```
2. Increase max replicas if needed:
   ```yaml
   # Edit k8s/keda-scaledobject-queue.yaml
   maxReplicaCount: 30  # Increase from 20
   ```
3. Apply changes:
   ```bash
   kubectl apply -f k8s/keda-scaledobject-queue.yaml
   ```

**If stuck at min replicas:**
1. Check if activation threshold is met:
   ```bash
   kubectl exec -n stockfish redis-0 -- redis-cli LLEN stockfish:jobs
   ```
2. Verify activation threshold in scaler config:
   ```yaml
   activationListLength: "1"  # Should be low
   ```

### Prevention
- Monitor KEDA and metrics-server health
- Set appropriate min/max replica bounds
- Test scaling behavior under load
- Review scaling events regularly

---

## Circuit Breaker Stuck Open

### Symptoms
- Circuit breaker state = 2 (open) for extended period
- Services returning errors immediately
- Half-open state not transitioning to closed

### Diagnosis

1. **Check Circuit Breaker State**
   ```promql
   circuit_breaker_state
   ```

2. **Check Failure Count**
   ```promql
   circuit_breaker_failures_total
   ```

3. **Check Dependency Health**
   ```bash
   # For Redis circuit breaker
   kubectl get pods -n stockfish -l app=redis
   kubectl exec -n stockfish redis-0 -- redis-cli PING
   
   # For Stockfish circuit breaker
   kubectl get pods -n stockfish -l app=stockfish
   kubectl exec -n stockfish -it <worker-pod> -- nc -zv stockfish 4000
   ```

4. **Check Service Logs**
   ```bash
   kubectl logs -n stockfish -l app=api --tail=100 | grep -i circuit
   kubectl logs -n stockfish -l app=worker --tail=100 | grep -i circuit
   ```

### Resolution

**If dependency is healthy:**
1. Wait for half-open state (30s timeout)
2. Monitor test connection attempts in logs
3. If test connections fail, investigate network issues:
   ```bash
   kubectl exec -n stockfish -it <api-pod> -- nc -zv redis 6379
   kubectl exec -n stockfish -it <worker-pod> -- nc -zv stockfish 4000
   ```

**If dependency is unhealthy:**
1. Restart unhealthy pods:
   ```bash
   kubectl delete pod -n stockfish <pod-name>
   ```
2. Wait for pod to become ready:
   ```bash
   kubectl wait --for=condition=ready pod -l app=<service> -n stockfish --timeout=60s
   ```
3. Circuit breaker will close automatically after successful test

**If circuit breaker configuration is too sensitive:**
1. Review failure threshold:
   ```go
   // API → Redis: 3 failures in 30s
   // Worker → Stockfish: 5 failures in 60s
   ```
2. Adjust if necessary in code and redeploy
3. Consider increasing timeout or failure threshold for flaky dependencies

### Prevention
- Monitor dependency health proactively
- Set up alerts for circuit breaker state changes
- Ensure adequate resource allocation for dependencies
- Test circuit breaker behavior in staging

---

## High Error Rate

### Symptoms
- Error rate > 5% for 2+ minutes
- Grafana alert: "High Error Rate"
- Increased 5xx responses

### Diagnosis

1. **Check Error Rate by Status Code**
   ```promql
   rate(api_requests_total{status_code=~"5.."}[5m]) / rate(api_requests_total[5m]) * 100
   ```

2. **Check Service Logs**
   ```bash
   kubectl logs -n stockfish -l app=api --tail=200 | grep '"level":"error"'
   kubectl logs -n stockfish -l app=worker --tail=200 | grep '"level":"error"'
   ```

3. **Check Circuit Breaker States**
   ```promql
   circuit_breaker_state
   ```

4. **Check Retry Attempts**
   ```promql
   rate(retry_attempts_total[5m])
   ```

### Resolution

**If errors are 503 (Service Unavailable):**
- See [API Service Unavailable](#api-service-unavailable-503)

**If errors are 500 (Internal Server Error):**
1. Check for panics or crashes in logs:
   ```bash
   kubectl logs -n stockfish -l app=api --tail=500 | grep -i "panic\|fatal"
   ```
2. Check for resource exhaustion:
   ```bash
   kubectl top pods -n stockfish
   ```
3. Restart affected pods:
   ```bash
   kubectl rollout restart deployment -n stockfish api
   ```

**If errors are intermittent:**
1. Check for network issues:
   ```bash
   kubectl exec -n stockfish -it <api-pod> -- ping redis
   ```
2. Check for DNS resolution issues:
   ```bash
   kubectl exec -n stockfish -it <api-pod> -- nslookup redis
   ```
3. Review retry metrics to see if retries are exhausted:
   ```promql
   retry_attempts_total{attempt_number="3"}
   ```

### Prevention
- Set up comprehensive error logging
- Monitor error rates by status code
- Implement proper error handling in code
- Test failure scenarios in staging

---

## Pod Crashes (CrashLoopBackOff)

### Symptoms
- Pods in CrashLoopBackOff state
- Pods restarting frequently
- Services unavailable

### Diagnosis

1. **Check Pod Status**
   ```bash
   kubectl get pods -n stockfish
   kubectl describe pod -n stockfish <pod-name>
   ```

2. **Check Pod Logs**
   ```bash
   kubectl logs -n stockfish <pod-name> --previous
   kubectl logs -n stockfish <pod-name> --tail=100
   ```

3. **Check Resource Usage**
   ```bash
   kubectl top pod -n stockfish <pod-name>
   ```

4. **Check Events**
   ```bash
   kubectl get events -n stockfish --sort-by='.lastTimestamp' | grep <pod-name>
   ```

### Resolution

**If OOMKilled (Out of Memory):**
1. Check memory limits:
   ```bash
   kubectl get deployment -n stockfish <service> -o yaml | grep -A 5 resources
   ```
2. Increase memory limits:
   ```yaml
   resources:
     limits:
       memory: "512Mi"  # Increase as needed
     requests:
       memory: "256Mi"
   ```
3. Apply changes:
   ```bash
   kubectl apply -f k8s/<service>-deployment.yaml
   ```

**If application error:**
1. Review logs for error messages:
   ```bash
   kubectl logs -n stockfish <pod-name> --previous | grep -i "error\|fatal\|panic"
   ```
2. Check for configuration issues (environment variables, secrets)
3. Verify dependencies are available (Redis, Stockfish)
4. Fix code issue and redeploy

**If liveness probe failing:**
1. Check liveness probe configuration:
   ```bash
   kubectl get deployment -n stockfish <service> -o yaml | grep -A 10 livenessProbe
   ```
2. Test health endpoint manually:
   ```bash
   kubectl exec -n stockfish -it <pod-name> -- curl localhost:8080/healthz
   ```
3. Adjust probe timing if service needs more startup time:
   ```yaml
   livenessProbe:
     initialDelaySeconds: 30  # Increase if needed
     periodSeconds: 10
     timeoutSeconds: 5
   ```

### Prevention
- Set appropriate resource requests and limits
- Implement proper health checks
- Test deployments in staging first
- Monitor pod restart counts

---

## Slow Queries / High Queue Depth

### Symptoms
- Queue depth > 100 for extended period
- Jobs taking longer than expected
- Workers scaled to max but queue still growing

### Diagnosis

1. **Check Queue Depth Trend**
   ```promql
   redis_queue_depth
   ```

2. **Check Worker Processing Rate**
   ```promql
   rate(worker_total_processing_seconds_count[5m])
   ```

3. **Check Engine Computation Time**
   ```promql
   histogram_quantile(0.95, rate(worker_engine_computation_seconds_bucket[5m]))
   ```

4. **Check for Slow Jobs**
   ```bash
   kubectl logs -n stockfish -l app=worker --tail=100 | grep "duration_ms"
   ```

### Resolution

**If jobs are legitimately slow:**
1. Check if high ELO or long time limits are requested
2. Consider implementing job prioritization
3. Add more Stockfish replicas:
   ```bash
   kubectl scale deployment -n stockfish stockfish --replicas=20
   ```

**If workers are slow to process:**
1. Check worker CPU/memory usage:
   ```bash
   kubectl top pods -n stockfish -l app=worker
   ```
2. Increase worker resources if needed
3. Check for network latency between worker and Stockfish:
   ```bash
   kubectl exec -n stockfish -it <worker-pod> -- ping stockfish
   ```

**If queue is growing faster than processing:**
1. Increase max worker replicas:
   ```yaml
   # k8s/keda-scaledobject-queue.yaml
   maxReplicaCount: 30
   ```
2. Decrease queue depth threshold for faster scaling:
   ```yaml
   listLength: "5"  # Scale more aggressively
   ```
3. Consider rate limiting at API level

### Prevention
- Monitor queue depth trends
- Set alerts for sustained high queue depth
- Load test to determine capacity limits
- Implement job prioritization for critical requests

---

## Metrics Not Appearing in Prometheus

### Symptoms
- Dashboards show "No data"
- Prometheus queries return empty results
- Metrics endpoints return data but Prometheus doesn't scrape

### Diagnosis

1. **Check Prometheus Targets**
   ```
   Navigate to Prometheus UI → Status → Targets
   http://<prometheus-url>:9090/targets
   ```

2. **Check Service Discovery**
   ```
   Navigate to Prometheus UI → Status → Service Discovery
   ```

3. **Test Metrics Endpoint**
   ```bash
   kubectl exec -n stockfish -it <api-pod> -- curl localhost:8080/metrics
   kubectl exec -n stockfish -it <worker-pod> -- curl localhost:9090/metrics
   ```

4. **Check Prometheus Logs**
   ```bash
   kubectl logs -n stockfish -l app=prometheus --tail=100
   ```

### Resolution

**If targets are down:**
1. Verify pods are running:
   ```bash
   kubectl get pods -n stockfish -l app=api
   kubectl get pods -n stockfish -l app=worker
   ```
2. Check if metrics ports are exposed:
   ```bash
   kubectl get svc -n stockfish
   ```
3. Test connectivity from Prometheus pod:
   ```bash
   kubectl exec -n stockfish -it <prometheus-pod> -- curl http://api:8080/metrics
   ```

**If service discovery not working:**
1. Check Prometheus configuration:
   ```bash
   kubectl get configmap -n stockfish prometheus-config -o yaml
   ```
2. Verify label selectors match pod labels:
   ```bash
   kubectl get pods -n stockfish --show-labels
   ```
3. Reload Prometheus configuration:
   ```bash
   kubectl exec -n stockfish -it <prometheus-pod> -- kill -HUP 1
   ```

**If metrics format is invalid:**
1. Check metrics endpoint output format
2. Verify Prometheus client library is correctly initialized
3. Check for duplicate metric names or invalid labels

### Prevention
- Monitor Prometheus target health
- Test metrics endpoints after deployments
- Use Prometheus client library validation
- Set up alerts for scrape failures

---

## Correlation ID Not Propagating

### Symptoms
- Logs missing correlation IDs
- Unable to trace requests across services
- Correlation ID in API but not in Worker logs

### Diagnosis

1. **Check API Response Headers**
   ```bash
   curl -v http://<api-url>/analyze -d '{"fen":"...","elo":1600}' | grep X-Correlation-ID
   ```

2. **Check Job Payload**
   ```bash
   kubectl exec -n stockfish redis-0 -- redis-cli LRANGE stockfish:jobs 0 0
   ```

3. **Check Worker Logs**
   ```bash
   kubectl logs -n stockfish -l app=worker --tail=50 | grep correlation_id
   ```

### Resolution

**If correlation ID not in API response:**
1. Check API middleware is installed
2. Verify correlation ID is added to response headers
3. Redeploy API service with fix

**If correlation ID not in job payload:**
1. Check API job creation code
2. Verify correlation ID is included in JSON payload
3. Redeploy API service with fix

**If correlation ID not in worker logs:**
1. Check worker log initialization
2. Verify correlation ID is extracted from job payload
3. Ensure logger is created with correlation ID context
4. Redeploy worker service with fix

### Prevention
- Test correlation ID flow in integration tests
- Add validation to ensure correlation ID is present
- Monitor logs for missing correlation IDs

---

## Emergency Procedures

### Complete System Outage

1. **Check cluster health:**
   ```bash
   kubectl get nodes
   kubectl get pods --all-namespaces
   ```

2. **Restart all services:**
   ```bash
   kubectl rollout restart deployment -n stockfish api
   kubectl rollout restart deployment -n stockfish worker
   kubectl rollout restart deployment -n stockfish stockfish
   kubectl delete pod -n stockfish redis-0
   ```

3. **Verify services are healthy:**
   ```bash
   kubectl wait --for=condition=ready pod -l app=api -n stockfish --timeout=120s
   kubectl wait --for=condition=ready pod -l app=worker -n stockfish --timeout=120s
   kubectl wait --for=condition=ready pod -l app=stockfish -n stockfish --timeout=120s
   ```

### Data Loss Prevention

1. **Backup Redis data:**
   ```bash
   kubectl exec -n stockfish redis-0 -- redis-cli SAVE
   kubectl cp stockfish/redis-0:/data/dump.rdb ./redis-backup-$(date +%Y%m%d-%H%M%S).rdb
   ```

2. **Backup Prometheus data:**
   ```bash
   kubectl exec -n stockfish <prometheus-pod> -- tar czf /tmp/prometheus-backup.tar.gz /prometheus
   kubectl cp stockfish/<prometheus-pod>:/tmp/prometheus-backup.tar.gz ./prometheus-backup-$(date +%Y%m%d-%H%M%S).tar.gz
   ```

## Support Contacts

- **Platform Team:** platform-team@example.com
- **On-Call:** Use PagerDuty escalation
- **Slack Channel:** #blunder-buss-support

## Related Documentation

- [Observability Guide](./observability-guide.md)
- [Cost Optimization Guide](./cost-optimization-guide.md)
- [Architecture Documentation](../README.md)
