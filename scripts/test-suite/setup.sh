#!/bin/bash
# Setup script for test suite

set -e

echo "========================================="
echo "Blunder-Buss Test Suite Setup"
echo "========================================="
echo ""

# Step 1: Install Python dependencies
echo "Step 1: Installing Python dependencies..."
if [ ! -d "venv" ]; then
    echo "Creating virtual environment..."
    python3 -m venv venv
fi

echo "Activating virtual environment..."
source venv/bin/activate

echo "Installing dependencies..."
pip install --upgrade pip
pip install -r requirements.txt

echo "✓ Python dependencies installed"
echo ""

# Step 2: Check for Kubernetes
echo "Step 2: Checking Kubernetes setup..."
echo ""

if command -v kubectl &> /dev/null; then
    echo "✓ kubectl found"
    
    if kubectl cluster-info &> /dev/null; then
        echo "✓ Kubernetes cluster is running"
        CLUSTER_TYPE=$(kubectl config current-context)
        echo "  Cluster: ${CLUSTER_TYPE}"
    else
        echo "✗ No Kubernetes cluster found"
        echo ""
        echo "You need a Kubernetes cluster. Choose one option:"
        echo ""
        echo "Option 1: Minikube (Local, Easy)"
        echo "  brew install minikube"
        echo "  minikube start --cpus=4 --memory=8192"
        echo ""
        echo "Option 2: Docker Desktop (Local, Easy)"
        echo "  1. Install Docker Desktop from docker.com"
        echo "  2. Enable Kubernetes in Settings > Kubernetes"
        echo ""
        echo "Option 3: Cloud (AWS EKS, GKE, AKS)"
        echo "  Follow cloud provider documentation"
        echo ""
        exit 1
    fi
else
    echo "✗ kubectl not found"
    echo ""
    echo "Install kubectl:"
    echo "  brew install kubectl"
    echo ""
    exit 1
fi

echo ""
echo "Step 3: Checking if Blunder-Buss is deployed..."
if kubectl get namespace stockfish &> /dev/null; then
    echo "✓ stockfish namespace exists"
    
    # Check if pods are running
    POD_COUNT=$(kubectl get pods -n stockfish --no-headers 2>/dev/null | wc -l)
    if [ "${POD_COUNT}" -gt 0 ]; then
        echo "✓ Found ${POD_COUNT} pods in stockfish namespace"
    else
        echo "⚠ No pods found. Need to deploy Blunder-Buss"
        echo ""
        echo "Deploy with:"
        echo "  kubectl apply -f ../../k8s/"
        echo ""
    fi
else
    echo "⚠ stockfish namespace not found"
    echo ""
    echo "Deploy Blunder-Buss:"
    echo "  kubectl create namespace stockfish"
    echo "  kubectl apply -f ../../k8s/"
    echo ""
fi

echo ""
echo "========================================="
echo "Setup Status"
echo "========================================="
echo "✓ Python dependencies: Installed"
echo ""
echo "Next steps:"
echo "1. Ensure Kubernetes cluster is running"
echo "2. Deploy Blunder-Buss: kubectl apply -f ../../k8s/"
echo "3. Run validation: ./validate-setup.sh"
echo "4. Run tests: ./run-all-tests.sh"
echo ""
echo "To activate virtual environment in future:"
echo "  source venv/bin/activate"
echo "========================================="
