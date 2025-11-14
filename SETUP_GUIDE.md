# Complete Setup Guide - Run Tests & Generate IEEE Paper Results

This guide will help you set up everything from scratch and run the tests to get real results for your IEEE paper.

## Current Status ✓

- ✓ Python 3.13.7 installed
- ✓ kubectl installed
- ✓ Docker Desktop installed
- ✗ Kubernetes not enabled in Docker Desktop
- ✗ Python dependencies not installed
- ✗ Blunder-Buss not deployed

## Step-by-Step Setup

### Step 1: Enable Kubernetes in Docker Desktop (5 minutes)

1. **Open Docker Desktop application**
   - Look for Docker icon in your menu bar
   - Click it and select "Dashboard"

2. **Enable Kubernetes**
   - Click the gear icon (⚙️) for Settings
   - Go to "Kubernetes" tab on the left
   - Check "Enable Kubernetes"
   - Click "Apply & Restart"
   - Wait 2-3 minutes for Kubernetes to start

3. **Verify Kubernetes is running**
   ```bash
   kubectl cluster-info
   # Should show: Kubernetes control plane is running
   ```

### Step 2: Install Python Dependencies (2 minutes)

```bash
cd scripts/test-suite

# Run setup script
./setup.sh
```

This will:
- Create a Python virtual environment
- Install all required packages
- Check your Kubernetes setup

### Step 3: Deploy Blunder-Buss (5 minutes)

```bash
# Create namespace
kubectl create namespace stockfish

# Deploy all components
kubectl apply -f k8s/

# Wait for pods to be ready (takes 2-3 minutes)
kubectl wait --for=condition=ready pod --all -n stockfish --timeout=300s

# Verify deployment
kubectl get pods -n stockfish
```

You should see pods for: api, worker, stockfish, redis, prometheus, grafana

### Step 4: Validate Setup (1 minute)

```bash
cd scripts/test-suite

# Activate virtual environment
source venv/bin/activate

# Run validation
./validate-setup.sh
```

All checks should pass ✓

### Step 5: Run Tests (2 hours)

```bash
# Make sure you're in the virtual environment
source venv/bin/activate

# Run complete test suite
./run-all-tests.sh
```

This will:
- Run load tests (30 min)
- Run autoscaling tests (30 min)
- Run fault tolerance tests (20 min)
- Run cost analysis (5 min)
- Run observability tests (5 min)
- Generate summary report and visualizations

Results will be saved to `results/{timestamp}/`

---

## Quick Start (If You're Impatient)

Want to see if everything works first? Run a quick 1-minute test:

```bash
# After completing Steps 1-4 above
source venv/bin/activate

# Quick test (1 minute)
python3 load_generator.py \
  --api-url http://localhost:30080 \
  --pattern constant \
  --rps 10 \
  --duration 60 \
  --output quick_test.json

# View results
cat quick_test.json | python3 -m json.tool
```

---

## Troubleshooting

### "Cannot connect to Kubernetes cluster"

**Solution:**
1. Check Docker Desktop is running
2. Enable Kubernetes in Docker Desktop settings
3. Wait for Kubernetes to fully start (green indicator)
4. Run: `kubectl cluster-info`

### "Pods not starting"

**Solution:**
```bash
# Check pod status
kubectl get pods -n stockfish

# Check specific pod logs
kubectl logs -n stockfish <pod-name>

# Common issue: Not enough resources
# Increase Docker Desktop resources:
# Settings > Resources > Set CPUs to 4, Memory to 8GB
```

### "Port already in use"

**Solution:**
```bash
# Kill existing port forwards
pkill -f "port-forward"

# Or find and kill specific process
lsof -ti:30080 | xargs kill -9
lsof -ti:9090 | xargs kill -9
```

### "Python module not found"

**Solution:**
```bash
# Make sure virtual environment is activated
source venv/bin/activate

# Reinstall dependencies
pip install -r requirements.txt
```

---

## Alternative: Use Minikube Instead of Docker Desktop

If Docker Desktop Kubernetes doesn't work, use Minikube:

```bash
# Install minikube
brew install minikube

# Start minikube with enough resources
minikube start --cpus=4 --memory=8192 --disk-size=20g

# Verify
kubectl cluster-info

# Then continue with Step 3 (Deploy Blunder-Buss)
```

---

## What Happens During Tests

### Load Tests (30 minutes)
- Sends requests at 10, 50, 100 req/s
- Measures latency (P50, P95, P99)
- Tracks error rates
- Validates throughput

### Autoscaling Tests (30 minutes)
- Generates load to trigger scaling
- Monitors replica counts
- Measures scale-up time
- Validates KEDA and HPA behavior

### Fault Tolerance Tests (20 minutes)
- Deletes 50% of Stockfish pods
- Tests circuit breakers
- Validates retry logic
- Measures recovery time

### Cost Analysis (5 minutes)
- Queries Prometheus for resource usage
- Calculates costs for 3 strategies
- Computes efficiency metrics
- Generates cost comparison charts

### Observability Tests (5 minutes)
- Measures instrumentation overhead
- Tests correlation ID propagation
- Validates metrics collection
- Confirms < 2ms overhead

---

## After Tests Complete

### View Results

```bash
# Navigate to results directory
cd results/{timestamp}

# View summary
cat summary_report.json | python3 -m json.tool

# View specific test results
cat autoscaling_results.json | python3 -m json.tool
cat cost_analysis_results.json | python3 -m json.tool

# View visualizations
open autoscaling_results.png
open cost_analysis_results.png
```

### Share Results With Me

Copy and paste the output of:

```bash
cat results/{timestamp}/summary_report.json
```

I'll then update the IEEE paper with your real results!

---

## Expected Timeline

| Task | Time | Status |
|------|------|--------|
| Enable Kubernetes | 5 min | ⏳ To Do |
| Install Python deps | 2 min | ⏳ To Do |
| Deploy Blunder-Buss | 5 min | ⏳ To Do |
| Validate setup | 1 min | ⏳ To Do |
| **Run tests** | **2 hours** | ⏳ To Do |
| **Total** | **~2 hours 15 min** | |

---

## Need Help?

If you get stuck at any step, share:
1. The step you're on
2. The error message
3. Output of: `kubectl get pods -n stockfish`

I'll help you troubleshoot!

---

## Ready to Start?

Run these commands in order:

```bash
# 1. Enable Kubernetes in Docker Desktop (manual step)
# 2. Verify Kubernetes
kubectl cluster-info

# 3. Install dependencies
cd scripts/test-suite
./setup.sh

# 4. Deploy system
kubectl create namespace stockfish
kubectl apply -f ../../k8s/

# 5. Wait for pods
kubectl wait --for=condition=ready pod --all -n stockfish --timeout=300s

# 6. Validate
source venv/bin/activate
./validate-setup.sh

# 7. Run tests
./run-all-tests.sh
```

Good luck! 🚀
