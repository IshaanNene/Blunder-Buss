#!/bin/bash
# Run realistic test suite adjusted for actual system capacity

set -e

TIMESTAMP=$(date +%Y%m%d_%H%M%S)
RESULTS_DIR="results/${TIMESTAMP}"
mkdir -p "${RESULTS_DIR}"

echo "========================================="
echo "Blunder-Buss Realistic Test Suite"
echo "========================================="
echo "Results will be saved to: ${RESULTS_DIR}"
echo ""

# Check prerequisites
echo "Checking prerequisites..."
if ! command -v kubectl &> /dev/null; then
    echo "Error: kubectl not found"
    exit 1
fi

if ! kubectl cluster-info &> /dev/null; then
    echo "Error: Cannot connect to Kubernetes cluster"
    exit 1
fi

echo "Prerequisites OK"
echo ""

# Scale up system for testing
echo "Scaling up system for testing..."
kubectl scale deployment worker -n stockfish --replicas=5
kubectl scale deployment stockfish -n stockfish --replicas=6
echo "Waiting for pods to be ready..."
sleep 45
echo ""

# Port forward Prometheus (run in background)
echo "Setting up port forwarding..."
kubectl port-forward -n stockfish svc/prometheus 9090:9090 &
PF_PID=$!
sleep 5

# Get API URL
API_URL="http://localhost:30080"
echo "API URL: ${API_URL}"
echo ""

# Test 1: Load Tests (Adjusted for realistic capacity)
echo "========================================="
echo "Test 1: Load Tests (Realistic)"
echo "========================================="
echo ""

echo "Running Workload A (Light - 5 req/s for 3 min)..."
python3 load_generator.py \
    --api-url "${API_URL}" \
    --pattern constant \
    --rps 5 \
    --duration 180 \
    --output "${RESULTS_DIR}/workload_a_light.json"

echo ""
echo "Running Workload B (Medium - 10 req/s for 3 min)..."
python3 load_generator.py \
    --api-url "${API_URL}" \
    --pattern constant \
    --rps 10 \
    --duration 180 \
    --output "${RESULTS_DIR}/workload_b_medium.json"

echo ""
echo "Running Workload C (Heavy - 15 req/s for 3 min)..."
python3 load_generator.py \
    --api-url "${API_URL}" \
    --pattern constant \
    --rps 15 \
    --duration 180 \
    --output "${RESULTS_DIR}/workload_c_heavy.json"

echo ""
echo "Running Workload D (Variable Load - 5 to 15 req/s)..."
python3 load_generator.py \
    --api-url "${API_URL}" \
    --pattern ramp \
    --rps 15 \
    --duration 300 \
    --output "${RESULTS_DIR}/workload_d_variable.json"

# Test 2: Observability Overhead
echo ""
echo "========================================="
echo "Test 2: Observability Overhead"
echo "========================================="
echo ""

python3 test_observability_overhead.py \
    --api-url "${API_URL}" \
    --output "${RESULTS_DIR}/observability_overhead_results.json"

# Generate summary report
echo ""
echo "========================================="
echo "Generating Summary Report"
echo "========================================="
echo ""

python3 generate_summary.py \
    --results-dir "${RESULTS_DIR}" \
    --output "${RESULTS_DIR}/summary_report.json"

# Cleanup
kill ${PF_PID} 2>/dev/null || true

echo ""
echo "========================================="
echo "Test Suite Complete!"
echo "========================================="
echo "Results saved to: ${RESULTS_DIR}"
echo ""
echo "Key files:"
echo "  - summary_report.json: Overall summary"
echo ""
echo "To view results:"
echo "  cat ${RESULTS_DIR}/summary_report.json | python3 -m json.tool"
echo ""
