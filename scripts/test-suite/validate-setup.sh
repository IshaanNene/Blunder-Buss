#!/bin/bash
# Validate that the system is ready for testing

set -e

echo "========================================="
echo "Blunder-Buss Setup Validation"
echo "========================================="
echo ""

ERRORS=0

# Check kubectl
echo -n "Checking kubectl... "
if command -v kubectl &> /dev/null; then
    echo "✓ Found"
else
    echo "✗ Not found"
    echo "  Install: https://kubernetes.io/docs/tasks/tools/"
    ERRORS=$((ERRORS + 1))
fi

# Check Python
echo -n "Checking Python 3... "
if command -v python3 &> /dev/null; then
    PYTHON_VERSION=$(python3 --version | cut -d' ' -f2)
    echo "✓ Found (${PYTHON_VERSION})"
else
    echo "✗ Not found"
    ERRORS=$((ERRORS + 1))
fi

# Check pip
echo -n "Checking pip... "
if command -v pip3 &> /dev/null; then
    echo "✓ Found"
else
    echo "✗ Not found"
    ERRORS=$((ERRORS + 1))
fi

# Check cluster connectivity
echo -n "Checking Kubernetes cluster... "
if kubectl cluster-info &> /dev/null; then
    CONTEXT=$(kubectl config current-context)
    echo "✓ Connected (${CONTEXT})"
else
    echo "✗ Cannot connect"
    echo "  Run: kubectl cluster-info"
    ERRORS=$((ERRORS + 1))
fi

# Check namespace
echo -n "Checking stockfish namespace... "
if kubectl get namespace stockfish &> /dev/null; then
    echo "✓ Exists"
else
    echo "✗ Not found"
    echo "  Create: kubectl create namespace stockfish"
    ERRORS=$((ERRORS + 1))
fi

# Check deployments
echo ""
echo "Checking deployments:"

DEPLOYMENTS=("api" "worker" "stockfish" "redis" "prometheus" "grafana")
for dep in "${DEPLOYMENTS[@]}"; do
    echo -n "  ${dep}... "
    if kubectl get deployment "${dep}" -n stockfish &> /dev/null 2>&1 || \
       kubectl get statefulset "${dep}" -n stockfish &> /dev/null 2>&1; then
        READY=$(kubectl get pods -n stockfish -l app="${dep}" -o jsonpath='{.items[*].status.conditions[?(@.type=="Ready")].status}' 2>/dev/null | grep -o "True" | wc -l)
        TOTAL=$(kubectl get pods -n stockfish -l app="${dep}" --no-headers 2>/dev/null | wc -l)
        if [ "${READY}" -eq "${TOTAL}" ] && [ "${TOTAL}" -gt 0 ]; then
            echo "✓ Ready (${READY}/${TOTAL})"
        else
            echo "⚠ Not ready (${READY}/${TOTAL})"
            ERRORS=$((ERRORS + 1))
        fi
    else
        echo "✗ Not found"
        ERRORS=$((ERRORS + 1))
    fi
done

# Check services
echo ""
echo "Checking services:"

SERVICES=("api" "worker" "stockfish" "redis" "prometheus" "grafana")
for svc in "${SERVICES[@]}"; do
    echo -n "  ${svc}... "
    if kubectl get service "${svc}" -n stockfish &> /dev/null; then
        echo "✓ Exists"
    else
        echo "✗ Not found"
        ERRORS=$((ERRORS + 1))
    fi
done

# Check Python dependencies
echo ""
echo "Checking Python dependencies:"

DEPS=("requests" "numpy" "pandas" "matplotlib" "prometheus_api_client" "kubernetes" "pyyaml" "tabulate" "colorama" "tqdm")
for dep in "${DEPS[@]}"; do
    echo -n "  ${dep}... "
    if python3 -c "import ${dep}" &> /dev/null; then
        echo "✓ Installed"
    else
        echo "✗ Not installed"
        echo "    Install: pip3 install ${dep}"
        ERRORS=$((ERRORS + 1))
    fi
done

# Test API connectivity
echo ""
echo -n "Testing API connectivity... "
API_URL="http://localhost:30080"
if curl -s -o /dev/null -w "%{http_code}" "${API_URL}/healthz" | grep -q "200"; then
    echo "✓ API responding"
else
    echo "⚠ API not accessible at ${API_URL}"
    echo "  Port forward: kubectl port-forward -n stockfish svc/api 30080:8080"
fi

# Test Prometheus connectivity
echo -n "Testing Prometheus connectivity... "
PROM_URL="http://localhost:9090"
if curl -s -o /dev/null -w "%{http_code}" "${PROM_URL}/api/v1/query?query=up" | grep -q "200"; then
    echo "✓ Prometheus responding"
else
    echo "⚠ Prometheus not accessible at ${PROM_URL}"
    echo "  Port forward: kubectl port-forward -n stockfish svc/prometheus 9090:9090"
fi

# Check KEDA
echo ""
echo -n "Checking KEDA installation... "
if kubectl get crd scaledobjects.keda.sh &> /dev/null; then
    echo "✓ Installed"
else
    echo "⚠ Not found (optional for autoscaling tests)"
fi

# Check HPA
echo -n "Checking HPA support... "
if kubectl get hpa -n stockfish &> /dev/null; then
    echo "✓ Available"
else
    echo "⚠ Not available"
fi

# Summary
echo ""
echo "========================================="
if [ ${ERRORS} -eq 0 ]; then
    echo "✓ All checks passed! Ready to run tests."
    echo ""
    echo "Next steps:"
    echo "  1. Review QUICKSTART.md for test instructions"
    echo "  2. Run: ./run-all-tests.sh"
    echo "  3. Or run individual tests as needed"
else
    echo "✗ ${ERRORS} issue(s) found. Please fix before running tests."
    echo ""
    echo "Common fixes:"
    echo "  1. Deploy system: kubectl apply -f ../../k8s/"
    echo "  2. Install dependencies: pip3 install -r requirements.txt"
    echo "  3. Port forward services (see above)"
fi
echo "========================================="
echo ""

exit ${ERRORS}
