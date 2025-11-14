#!/bin/bash
# Run complete test suite for IEEE paper validation

set -e

TIMESTAMP=$(date +%Y%m%d_%H%M%S)
RESULTS_DIR="results/${TIMESTAMP}"
mkdir -p "${RESULTS_DIR}"

echo "========================================="
echo "Blunder-Buss Test Suite"
echo "========================================="
echo "Results will be saved to: ${RESULTS_DIR}"
echo ""

# Check prerequisites
echo "Checking prerequisites..."
if ! command -v kubectl &> /dev/null; then
    echo "Error: kubectl not found"
    exit 1
fi

if ! command -v python3 &> /dev/null; then
    echo "Error: python3 not found"
    exit 1
fi

# Check cluster connectivity
if ! kubectl cluster-info &> /dev/null; then
    echo "Error: Cannot connect to Kubernetes cluster"
    exit 1
fi

# Check if system is deployed
if ! kubectl get pods -n stockfish &> /dev/null; then
    echo "Error: Blunder-Buss not deployed in stockfish namespace"
    exit 1
fi

echo "Prerequisites OK"
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

# Test 1: Load Tests
echo "========================================="
echo "Test 1: Load Tests"
echo "========================================="
echo ""

echo "Running Workload A (Light - 10 req/s)..."
python3 load_generator.py \
    --api-url "${API_URL}" \
    --pattern constant \
    --rps 10 \
    --duration 300 \
    --output "${RESULTS_DIR}/workload_a_light.json"

echo ""
echo "Running Workload B (Medium - 50 req/s)..."
python3 load_generator.py \
    --api-url "${API_URL}" \
    --pattern constant \
    --rps 50 \
    --duration 300 \
    --output "${RESULTS_DIR}/workload_b_medium.json"

echo ""
echo "Running Workload C (Heavy - 100 req/s)..."
python3 load_generator.py \
    --api-url "${API_URL}" \
    --pattern constant \
    --rps 100 \
    --duration 300 \
    --output "${RESULTS_DIR}/workload_c_heavy.json"

echo ""
echo "Running Workload D (Variable Load)..."
python3 load_generator.py \
    --api-url "${API_URL}" \
    --pattern ramp \
    --rps 100 \
    --duration 600 \
    --output "${RESULTS_DIR}/workload_d_variable.json"

echo ""
echo "Running Workload E (Spike Load)..."
python3 load_generator.py \
    --api-url "${API_URL}" \
    --pattern spike \
    --duration 1800 \
    --output "${RESULTS_DIR}/workload_e_spike.json"

# Test 2: Autoscaling Tests
echo ""
echo "========================================="
echo "Test 2: Autoscaling Tests"
echo "========================================="
echo ""

python3 test_autoscaling.py \
    --namespace stockfish \
    --prometheus-url http://localhost:9090 \
    --test all \
    --output "${RESULTS_DIR}/autoscaling_results.json"

# Test 3: Fault Tolerance Tests
echo ""
echo "========================================="
echo "Test 3: Fault Tolerance Tests"
echo "========================================="
echo ""

python3 test_fault_tolerance.py \
    --namespace stockfish \
    --prometheus-url http://localhost:9090 \
    --test all \
    --output "${RESULTS_DIR}/fault_tolerance_results.json"

# Test 4: Cost Analysis
echo ""
echo "========================================="
echo "Test 4: Cost Analysis"
echo "========================================="
echo ""

python3 test_cost_analysis.py \
    --namespace stockfish \
    --prometheus-url http://localhost:9090 \
    --hours 24 \
    --output "${RESULTS_DIR}/cost_analysis_results.json"

# Test 5: Observability Overhead
echo ""
echo "========================================="
echo "Test 5: Observability Overhead"
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
echo "  - autoscaling_results.png: Scaling visualizations"
echo "  - cost_analysis_results.png: Cost comparisons"
echo ""
echo "To view results:"
echo "  cat ${RESULTS_DIR}/summary_report.json | jq"
echo ""
