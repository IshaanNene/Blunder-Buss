# Blunder-Buss Test Suite

This directory contains comprehensive test scripts to validate the performance claims in the IEEE paper.

## Test Categories

1. **Load Tests** - Measure latency, throughput, and error rates under various loads
2. **Autoscaling Tests** - Validate KEDA and HPA scaling behavior
3. **Fault Tolerance Tests** - Test circuit breakers and retry logic
4. **Cost Analysis** - Calculate actual infrastructure costs
5. **Observability Tests** - Measure instrumentation overhead

## Prerequisites

```bash
# Install required tools
pip install -r requirements.txt

# Ensure kubectl is configured
kubectl cluster-info

# Ensure the system is deployed
kubectl get pods -n stockfish
```

## Running All Tests

```bash
# Run complete test suite (takes ~2 hours)
./run-all-tests.sh

# Run specific test category
./run-load-tests.sh
./run-autoscaling-tests.sh
./run-fault-tolerance-tests.sh
./run-cost-analysis.sh
```

## Results

Test results are saved to `results/` directory with timestamps.
Summary reports are generated in `results/summary-{timestamp}.json`
