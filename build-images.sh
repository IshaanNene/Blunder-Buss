#!/bin/bash
# Build all Docker images for Blunder-Buss

set -e

echo "========================================="
echo "Building Blunder-Buss Docker Images"
echo "========================================="
echo ""

# Build API
echo "Building API image..."
docker build -f docker/api/Dockerfile -t api:latest .
echo "✓ API image built"
echo ""

# Build Worker
echo "Building Worker image..."
docker build -f docker/worker/Dockerfile -t worker:latest .
echo "✓ Worker image built"
echo ""

# Build Stockfish
echo "Building Stockfish image..."
docker build -f docker/stockfish/Dockerfile -t stockfish:latest .
echo "✓ Stockfish image built"
echo ""

# Build Web (if needed)
if [ -f "docker/web/Dockerfile" ]; then
    echo "Building Web image..."
    docker build -f docker/web/Dockerfile -t web:latest .
    echo "✓ Web image built"
    echo ""
fi

echo "========================================="
echo "✓ All images built successfully!"
echo "========================================="
echo ""
echo "Images created:"
docker images | grep -E "api|worker|stockfish|web" | grep latest
echo ""
echo "Next steps:"
echo "1. Deploy to Kubernetes: kubectl apply -f k8s/"
echo "2. Wait for pods: kubectl wait --for=condition=ready pod --all -n stockfish --timeout=300s"
echo "3. Run tests: cd scripts/test-suite && ./run-all-tests.sh"
