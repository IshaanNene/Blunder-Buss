# Setup Checklist

Follow this checklist to get your tests running:

## ☐ Step 1: Enable Kubernetes (5 minutes)

1. Open Docker Desktop
2. Click Settings (gear icon)
3. Go to Kubernetes tab
4. Check "Enable Kubernetes"
5. Click "Apply & Restart"
6. Wait for green indicator

**Verify:**
```bash
kubectl cluster-info
# Should show: Kubernetes control plane is running at...
```

---

## ☐ Step 2: Install Python Dependencies (2 minutes)

```bash
cd scripts/test-suite
./setup.sh
```

**Verify:**
```bash
source venv/bin/activate
python3 -c "import requests; print('✓ Dependencies installed')"
```

---

## ☐ Step 3: Deploy Blunder-Buss (5 minutes)

```bash
# Create namespace
kubectl create namespace stockfish

# Deploy everything
kubectl apply -f ../../k8s/

# Wait for pods (2-3 minutes)
kubectl wait --for=condition=ready pod --all -n stockfish --timeout=300s
```

**Verify:**
```bash
kubectl get pods -n stockfish
# Should show: api, worker, stockfish, redis, prometheus, grafana pods
# All should be Running with 1/1 or 2/2 Ready
```

---

## ☐ Step 4: Validate Setup (1 minute)

```bash
cd scripts/test-suite
source venv/bin/activate
./validate-setup.sh
```

**Expected:** All checks should pass ✓

---

## ☐ Step 5: Run Tests (2 hours)

```bash
source venv/bin/activate
./run-all-tests.sh
```

**This will:**
- ✓ Run 5 workload patterns
- ✓ Test autoscaling behavior
- ✓ Test fault tolerance
- ✓ Calculate costs
- ✓ Measure observability overhead
- ✓ Generate summary report
- ✓ Create visualization charts

**Results saved to:** `results/{timestamp}/`

---

## ☐ Step 6: Share Results

```bash
# Copy this output and share with me
cat results/{timestamp}/summary_report.json
```

I'll update the IEEE paper with your real results!

---

## Quick Commands Reference

```bash
# Check Kubernetes
kubectl cluster-info
kubectl get pods -n stockfish

# Activate Python environment
source venv/bin/activate

# View logs
kubectl logs -n stockfish -l app=api --tail=50

# Port forward for manual testing
kubectl port-forward -n stockfish svc/api 30080:8080 &
kubectl port-forward -n stockfish svc/prometheus 9090:9090 &

# Test API manually
curl http://localhost:30080/healthz

# Clean up and restart
kubectl delete namespace stockfish
kubectl create namespace stockfish
kubectl apply -f ../../k8s/
```

---

## Current Status

- [ ] Kubernetes enabled
- [ ] Python dependencies installed
- [ ] Blunder-Buss deployed
- [ ] Setup validated
- [ ] Tests running
- [ ] Results generated

**Next:** Enable Kubernetes in Docker Desktop!
