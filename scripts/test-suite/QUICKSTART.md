# Quick Start Guide - Test Suite

This guide will help you run the complete test suite to validate all claims in the IEEE paper.

## Prerequisites

1. **Kubernetes Cluster Running**
   ```bash
   kubectl cluster-info
   ```

2. **Blunder-Buss Deployed**
   ```bash
   kubectl get pods -n stockfish
   # Should show: api, worker, stockfish, redis, prometheus, grafana pods
   ```

3. **Python Dependencies**
   ```bash
   cd scripts/test-suite
   pip install -r requirements.txt
   ```

4. **Port Forwarding** (for local testing)
   ```bash
   # API Service
   kubectl port-forward -n stockfish svc/api 30080:8080 &
   
   # Prometheus
   kubectl port-forward -n stockfish svc/prometheus 9090:9090 &
   ```

## Running Tests

### Option 1: Run Complete Test Suite (Recommended)

This runs all tests and generates a comprehensive report (~2 hours):

```bash
./run-all-tests.sh
```

Results will be saved to `results/{timestamp}/`

### Option 2: Run Individual Tests

**Load Tests** (30 minutes):
```bash
# Light load (10 req/s)
python3 load_generator.py --api-url http://localhost:30080 --pattern constant --rps 10 --duration 300 --output results/light.json

# Medium load (50 req/s)
python3 load_generator.py --api-url http://localhost:30080 --pattern constant --rps 50 --duration 300 --output results/medium.json

# Heavy load (100 req/s)
python3 load_generator.py --api-url http://localhost:30080 --pattern constant --rps 100 --duration 300 --output results/heavy.json
```

**Autoscaling Tests** (30 minutes):
```bash
python3 test_autoscaling.py --namespace stockfish --prometheus-url http://localhost:9090 --test all --output results/autoscaling.json
```

**Fault Tolerance Tests** (20 minutes):
```bash
python3 test_fault_tolerance.py --namespace stockfish --prometheus-url http://localhost:9090 --test all --output results/fault_tolerance.json
```

**Cost Analysis** (5 minutes):
```bash
python3 test_cost_analysis.py --namespace stockfish --prometheus-url http://localhost:9090 --hours 24 --output results/cost.json
```

**Observability Overhead** (5 minutes):
```bash
python3 test_observability_overhead.py --api-url http://localhost:30080 --output results/overhead.json
```

## Understanding Results

### Load Test Results

```json
{
  "analysis": {
    "total_requests": 3000,
    "successful_requests": 2997,
    "error_rate": 0.1,
    "throughput_rps": 49.8,
    "latency": {
      "p50": 1234.5,
      "p95": 4567.8,
      "p99": 6789.0
    }
  }
}
```

**Key Metrics:**
- `error_rate`: Should be < 1%
- `latency.p95`: Should be < 5000ms (5 seconds)
- `throughput_rps`: Should match target load

### Autoscaling Results

```json
{
  "test": "worker_scale_up",
  "initial_replicas": 1,
  "final_replicas": 10,
  "avg_scale_time_seconds": 28.5,
  "scale_events": 9
}
```

**Key Metrics:**
- `avg_scale_time_seconds`: Should be < 30s (paper claim)
- `scale_events`: Number of scaling operations
- Visual plots show replica count over time

### Fault Tolerance Results

```json
{
  "test": "stockfish_failure",
  "during_failure": {
    "max_error_rate": 2.3,
    "circuit_opened": true
  },
  "recovery_time_seconds": 33,
  "throughput_maintained_percent": 98
}
```

**Key Metrics:**
- `max_error_rate`: Should be < 5% during failure
- `circuit_opened`: Should be true (circuit breaker working)
- `recovery_time_seconds`: Should be < 60s
- `throughput_maintained_percent`: Should be > 95%

### Cost Analysis Results

```json
{
  "optimized": {
    "total_cost": 1120.50,
    "cost_per_1m_requests": 11.20,
    "savings_vs_static_percent": 54.2
  }
}
```

**Key Metrics:**
- `savings_vs_static_percent`: Should be 40-60% (paper claim)
- `cost_per_1m_requests`: Lower is better
- Plots show cost breakdown by component

### Observability Overhead Results

```json
{
  "total_overhead_ms": {
    "mean": 1.15,
    "p95": 1.78,
    "p99": 2.01
  },
  "overhead_percentage": 0.18,
  "meets_paper_claim": true
}
```

**Key Metrics:**
- `total_overhead_ms.mean`: Should be < 2ms (paper claim)
- `overhead_percentage`: Should be < 1%
- `meets_paper_claim`: Should be true

## Viewing Results

### Summary Report

```bash
# View JSON summary
cat results/{timestamp}/summary_report.json | jq

# View specific metrics
cat results/{timestamp}/summary_report.json | jq '.cost_analysis'
```

### Visualizations

Generated PNG files:
- `autoscaling_results.png`: Replica counts and queue depth over time
- `cost_analysis_results.png`: Cost comparison charts

```bash
# View on Mac
open results/{timestamp}/autoscaling_results.png
open results/{timestamp}/cost_analysis_results.png

# View on Linux
xdg-open results/{timestamp}/autoscaling_results.png
```

## Troubleshooting

### "Cannot connect to Kubernetes cluster"

```bash
# Check cluster status
kubectl cluster-info

# Check context
kubectl config current-context

# If using minikube
minikube status
```

### "Blunder-Buss not deployed"

```bash
# Deploy the system
kubectl apply -f k8s/

# Wait for pods to be ready
kubectl wait --for=condition=ready pod -l app=api -n stockfish --timeout=300s
```

### "Connection refused to API"

```bash
# Check API service
kubectl get svc -n stockfish api

# Port forward manually
kubectl port-forward -n stockfish svc/api 30080:8080

# Test connection
curl http://localhost:30080/healthz
```

### "Prometheus metrics not available"

```bash
# Check Prometheus pod
kubectl get pods -n stockfish -l app=prometheus

# Port forward Prometheus
kubectl port-forward -n stockfish svc/prometheus 9090:9090

# Test Prometheus
curl http://localhost:9090/api/v1/query?query=up
```

### "Tests taking too long"

You can reduce test duration for quick validation:

```bash
# Quick load test (1 minute instead of 5)
python3 load_generator.py --api-url http://localhost:30080 --pattern constant --rps 50 --duration 60 --output results/quick.json

# Quick autoscaling test (5 minutes instead of 10)
python3 test_autoscaling.py --test scale-up --output results/quick_autoscaling.json
```

## Validating Paper Claims

### Claim 1: P95 Latency < 5 seconds

```bash
# Check load test results
cat results/{timestamp}/workload_b_medium.json | jq '.analysis.latency.p95'
# Should be < 5000ms
```

### Claim 2: 54% Cost Reduction

```bash
# Check cost analysis
cat results/{timestamp}/cost_analysis_results.json | jq '.optimized.savings_vs_static_percent'
# Should be 40-60%
```

### Claim 3: 30-second Scale-up Time

```bash
# Check autoscaling results
cat results/{timestamp}/autoscaling_results.json | jq '.[0].avg_scale_time_seconds'
# Should be < 30s
```

### Claim 4: 99.5% Uptime During Failures

```bash
# Check fault tolerance results
cat results/{timestamp}/fault_tolerance_results.json | jq '.[0].throughput_maintained_percent'
# Should be > 98%
```

### Claim 5: < 2ms Observability Overhead

```bash
# Check observability overhead
cat results/{timestamp}/observability_overhead_results.json | jq '.total_overhead_ms.mean'
# Should be < 2ms
```

## Next Steps

1. **Analyze Results**: Review the summary report and visualizations
2. **Update Paper**: Use actual results to update experimental section
3. **Run Long-term Tests**: For production validation, run tests for 24-48 hours
4. **Compare Environments**: Run tests on different cloud providers (AWS, GCP, Azure)

## Support

For issues or questions:
1. Check logs: `kubectl logs -n stockfish -l app=api --tail=100`
2. Check metrics: Open Grafana at `http://localhost:30300`
3. Review test output files in `results/` directory
