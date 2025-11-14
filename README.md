# Blunder-Buss

<table>
  <tr>
    <td>
      <img width="200" height="200" src="https://github.com/user-attachments/assets/8b04a95e-fbc1-4edd-bfd6-1c5286e9ca0c" alt="logo"/>
    </td>
    <td>
      <blockquote>
        A distributed, scalable, fault tolerant Chess Platform  
        Built using Docker to run Stockfish engine on containers  
        Kubernetes to deploy, scale, and manage these containers  
        Redis as an in-memory message queue for chess jobs
      </blockquote>
    </td>
  </tr>
</table>

## Architecture Overview

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

## Features

- **Distributed Architecture:** Microservices-based design with API, Worker, and Stockfish services
- **Auto-Scaling:** Intelligent scaling based on queue depth, latency, and CPU utilization
- **Fault Tolerance:** Circuit breakers and retry logic with exponential backoff
- **Observability:** Comprehensive metrics, structured logging, and distributed tracing
- **Cost Optimization:** Detailed cost tracking and efficiency metrics
- **High Availability:** Graceful shutdown, health checks, and redundancy

## Documentation

- **[Observability Guide](docs/observability-guide.md)** - Metrics, dashboards, logging, and alerting
- **[Troubleshooting Runbook](docs/troubleshooting-runbook.md)** - Common issues and resolution procedures
- **[Cost Optimization Guide](docs/cost-optimization-guide.md)** - Strategies for reducing infrastructure costs

## Quick Start

### Prerequisites

- Kubernetes cluster (EKS, GKE, or local with minikube)
- kubectl configured
- Docker for building images
- KEDA installed for event-driven autoscaling

### Deploy to Kubernetes

```bash
# Apply all Kubernetes manifests
kubectl apply -f k8s/

# Verify deployment
kubectl get pods -n stockfish
kubectl get svc -n stockfish
```

### Access Services

- **API Service:** `http://<node-ip>:30080`
- **Grafana Dashboard:** `http://<node-ip>:30300`
- **Prometheus:** `http://<node-ip>:30090`

### Test the API

```bash
curl -X POST http://<node-ip>:30080/analyze \
  -H "Content-Type: application/json" \
  -d '{
    "fen": "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1",
    "elo": 1600,
    "max_time_ms": 1000
  }'
```

## Monitoring

### Grafana Dashboards

Access Grafana at `http://<node-ip>:30300` to view:

1. **System Overview** - Request rate, latency percentiles, error rate, queue depth
2. **Auto-Scaling Metrics** - Replica counts, scaling events, threshold visualization
3. **Fault Tolerance** - Circuit breaker states, retry attempts, connection failures
4. **Cost Efficiency** - Operations per CPU-second, estimated costs, idle time

### Key Metrics

- **API Latency (P95):** Target < 5 seconds
- **Error Rate:** Target < 1%
- **Queue Depth:** Scales workers at 10 jobs per replica
- **Cost Efficiency:** Target > 0.5 operations per CPU-second
- **Worker Idle Time:** Target < 20%

### Alerts

Prometheus alerts are configured for:
- High API latency (P95 > 10s for 5 minutes)
- High error rate (> 5% for 2 minutes)
- Circuit breaker open states
- High queue depth (> 100 for 5 minutes)

## Repo Structure
<img width="641" height="584" alt="image" src="https://github.com/user-attachments/assets/198fa812-e60a-4e30-af72-6ad214559c7f" />

## Development

### Building Images

```bash
# Build all images
make build

# Build specific service
docker build -f docker/api/Dockerfile -t blunder-buss-api:latest .
docker build -f docker/worker/Dockerfile -t blunder-buss-worker:latest .
docker build -f docker/stockfish/Dockerfile -t blunder-buss-stockfish:latest .
```

### Local Development

```bash
# Start services locally with Docker Compose
docker-compose -f docker-compose.local.yml up

# Run API locally
cd go/api
go run main.go

# Run Worker locally
cd go/worker
go run main.go
```

### Testing

```bash
# Run tests for shared packages
cd go/pkg
go test ./...

# Run API tests
cd go/api
go test ./...

# Run Worker tests
cd go/worker
go test ./...
```

## Auto-Scaling Configuration

### Worker Scaling (KEDA)

- **Trigger:** Redis queue depth
- **Threshold:** 10 jobs per replica
- **Min Replicas:** 1
- **Max Replicas:** 20
- **Scale-up Cooldown:** 30 seconds
- **Scale-down Cooldown:** 5 minutes

### Stockfish Scaling (HPA)

- **Trigger:** CPU utilization
- **Threshold:** 75%
- **Min Replicas:** 2
- **Max Replicas:** 15
- **Scale-up Policy:** 100% or 2 pods per 60s
- **Scale-down Policy:** 25% per 120s with 10-minute stabilization

### API Scaling (HPA)

- **Trigger:** CPU utilization
- **Threshold:** 70%
- **Min Replicas:** 2 (high availability)
- **Max Replicas:** 10

## Fault Tolerance

### Circuit Breakers

- **API → Redis:** Opens after 3 failures in 30s, 30s timeout
- **Worker → Stockfish:** Opens after 5 failures in 60s, 30s timeout

### Retry Logic

- **Worker → Stockfish:** 3 attempts, exponential backoff (100ms-5s), 20% jitter
- **API → Redis:** 2 attempts, 50ms fixed delay
- **Worker → Redis:** 3 attempts, exponential backoff with jitter

### Graceful Shutdown

- API and Worker services complete in-flight requests before terminating
- Maximum shutdown timeout: 30 seconds
- Signal handlers for SIGTERM and SIGINT

## Cost Optimization

See the [Cost Optimization Guide](docs/cost-optimization-guide.md) for detailed strategies. Key recommendations:

- Use spot instances for Worker and Stockfish services (60-70% savings)
- Right-size resource requests to P95 usage + 20% buffer
- Scale workers to 0 during off-peak hours
- Implement caching for repeated positions (20-40% compute reduction)
- Monitor cost efficiency ratio (target > 0.5 operations per CPU-second)

**Expected Total Savings:** 40-60% of infrastructure costs

## Troubleshooting

For common issues and resolution procedures, see the [Troubleshooting Runbook](docs/troubleshooting-runbook.md).

Quick troubleshooting commands:

```bash
# Check pod status
kubectl get pods -n stockfish

# View logs
kubectl logs -n stockfish -l app=api --tail=100
kubectl logs -n stockfish -l app=worker --tail=100

# Check metrics
kubectl port-forward -n stockfish svc/prometheus 9090:9090
# Open http://localhost:9090

# Check circuit breaker states
kubectl logs -n stockfish -l app=api | grep circuit

# Check queue depth
kubectl exec -n stockfish redis-0 -- redis-cli LLEN stockfish:jobs
```

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

See [LICENSE](LICENSE) file for details.


