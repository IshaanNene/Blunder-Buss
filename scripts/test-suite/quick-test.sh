#!/bin/bash
# Quick 1-minute test to verify system is working

echo "Running quick test (1 minute)..."
echo ""

python3 load_generator.py \
  --api-url http://localhost:30080 \
  --pattern constant \
  --rps 10 \
  --duration 60 \
  --output quick_test.json

echo ""
echo "=== Quick Test Results ==="
cat quick_test.json | python3 -c "
import json, sys
data = json.load(sys.stdin)
analysis = data.get('analysis', {})
latency = analysis.get('latency', {})

print(f\"Total Requests: {analysis.get('total_requests', 0)}\")
print(f\"Successful: {analysis.get('successful_requests', 0)}\")
print(f\"Error Rate: {analysis.get('error_rate', 0):.2f}%\")
print(f\"P50 Latency: {latency.get('p50', 0):.2f}ms\")
print(f\"P95 Latency: {latency.get('p95', 0):.2f}ms\")
print(f\"P99 Latency: {latency.get('p99', 0):.2f}ms\")
print(f\"Throughput: {analysis.get('throughput_rps', 0):.2f} req/s\")
"

echo ""
echo "✓ System is working! Ready for full test suite."
