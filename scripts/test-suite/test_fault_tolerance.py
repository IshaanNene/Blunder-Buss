#!/usr/bin/env python3
"""
Fault Tolerance Test Suite
Tests circuit breakers, retry logic, and system resilience
"""

import time
import json
import subprocess
import argparse
from typing import Dict, List
from kubernetes import client, config
from prometheus_api_client import PrometheusConnect
import requests


class FaultToleranceTester:
    def __init__(self, namespace: str = "stockfish", prometheus_url: str = "http://localhost:9090"):
        config.load_kube_config()
        self.v1 = client.CoreV1Api()
        self.apps_v1 = client.AppsV1Api()
        self.namespace = namespace
        self.prom = PrometheusConnect(url=prometheus_url, disable_ssl=True)
    
    def get_circuit_breaker_state(self, service: str) -> float:
        """Get circuit breaker state from Prometheus (0=closed, 1=half-open, 2=open)"""
        query = f'circuit_breaker_state{{service="{service}",namespace="{self.namespace}"}}'
        result = self.prom.custom_query(query=query)
        if result:
            return float(result[0]['value'][1])
        return 0.0
    
    def get_error_rate(self) -> float:
        """Get current error rate from Prometheus"""
        query = 'rate(api_requests_total{status_code=~"5.."}[1m]) / rate(api_requests_total[1m]) * 100'
        result = self.prom.custom_query(query=query)
        if result:
            return float(result[0]['value'][1])
        return 0.0
    
    def get_retry_count(self) -> int:
        """Get total retry attempts from Prometheus"""
        query = 'sum(retry_attempts_total)'
        result = self.prom.custom_query(query=query)
        if result:
            return int(float(result[0]['value'][1]))
        return 0
    
    def get_throughput(self) -> float:
        """Get current throughput (req/s)"""
        query = 'rate(api_requests_total[1m])'
        result = self.prom.custom_query(query=query)
        if result:
            return float(result[0]['value'][1])
        return 0.0
    
    def delete_pods(self, deployment: str, percentage: int = 50):
        """Delete a percentage of pods for a deployment"""
        pods = self.v1.list_namespaced_pod(
            self.namespace,
            label_selector=f"app={deployment}"
        )
        
        num_to_delete = max(1, int(len(pods.items) * percentage / 100))
        deleted = []
        
        for i, pod in enumerate(pods.items[:num_to_delete]):
            print(f"Deleting pod: {pod.metadata.name}")
            self.v1.delete_namespaced_pod(pod.metadata.name, self.namespace)
            deleted.append(pod.metadata.name)
        
        return deleted
    
    def test_stockfish_failure(self, duration: int = 300) -> Dict:
        """Test circuit breaker behavior when Stockfish pods fail"""
        print("\n=== Testing Stockfish Failure Scenario ===")
        
        # Start baseline load
        print("Starting baseline load (50 req/s)...")
        load_cmd = "python3 load_generator.py --api-url http://localhost:30080 --pattern constant --rps 50 --duration 300 --output /tmp/fault_test_load.json &"
        subprocess.Popen(load_cmd, shell=True)
        
        time.sleep(10)  # Let load stabilize
        
        # Collect baseline metrics
        print("Collecting baseline metrics...")
        baseline = {
            "error_rate": self.get_error_rate(),
            "throughput": self.get_throughput(),
            "circuit_state": self.get_circuit_breaker_state("stockfish"),
            "retry_count": self.get_retry_count()
        }
        print(f"Baseline - Error rate: {baseline['error_rate']:.2f}%, Throughput: {baseline['throughput']:.2f} req/s")
        
        # Inject failure
        print("\nInjecting failure: Deleting 50% of Stockfish pods...")
        failure_time = time.time()
        deleted_pods = self.delete_pods("stockfish", percentage=50)
        
        # Monitor during failure
        metrics = []
        for i in range(60):  # Monitor for 60 seconds
            time.sleep(1)
            elapsed = time.time() - failure_time
            
            metric = {
                "elapsed": elapsed,
                "error_rate": self.get_error_rate(),
                "throughput": self.get_throughput(),
                "circuit_state": self.get_circuit_breaker_state("stockfish"),
                "retry_count": self.get_retry_count()
            }
            metrics.append(metric)
            
            if i % 10 == 0:
                print(f"  [{elapsed:.0f}s] Error rate: {metric['error_rate']:.2f}%, "
                      f"Circuit: {metric['circuit_state']}, "
                      f"Throughput: {metric['throughput']:.2f} req/s")
        
        # Wait for recovery
        print("\nWaiting for recovery...")
        time.sleep(60)
        
        # Collect recovery metrics
        recovery = {
            "error_rate": self.get_error_rate(),
            "throughput": self.get_throughput(),
            "circuit_state": self.get_circuit_breaker_state("stockfish"),
            "retry_count": self.get_retry_count()
        }
        print(f"Recovery - Error rate: {recovery['error_rate']:.2f}%, Throughput: {recovery['throughput']:.2f} req/s")
        
        # Analyze results
        max_error_rate = max(m['error_rate'] for m in metrics)
        min_throughput = min(m['throughput'] for m in metrics)
        circuit_opened = any(m['circuit_state'] == 2.0 for m in metrics)
        
        # Find recovery time (when circuit closes)
        recovery_time = None
        for m in metrics:
            if m['circuit_state'] == 0.0 and m['elapsed'] > 30:
                recovery_time = m['elapsed']
                break
        
        result = {
            "test": "stockfish_failure",
            "deleted_pods": deleted_pods,
            "baseline": baseline,
            "during_failure": {
                "max_error_rate": max_error_rate,
                "min_throughput": min_throughput,
                "circuit_opened": circuit_opened
            },
            "recovery": recovery,
            "recovery_time_seconds": recovery_time,
            "metrics": metrics
        }
        
        print(f"\n=== Results ===")
        print(f"Max error rate during failure: {max_error_rate:.2f}%")
        print(f"Min throughput during failure: {min_throughput:.2f} req/s")
        print(f"Circuit breaker opened: {circuit_opened}")
        print(f"Recovery time: {recovery_time:.1f}s" if recovery_time else "Did not recover")
        print(f"Throughput maintained: {min_throughput / baseline['throughput'] * 100:.1f}%")
        
        return result
    
    def test_redis_failure(self, duration: int = 180) -> Dict:
        """Test circuit breaker behavior when Redis fails"""
        print("\n=== Testing Redis Failure Scenario ===")
        
        # Start load
        print("Starting load (30 req/s)...")
        load_cmd = "python3 load_generator.py --api-url http://localhost:30080 --pattern constant --rps 30 --duration 180 --output /tmp/redis_fault_load.json &"
        subprocess.Popen(load_cmd, shell=True)
        
        time.sleep(10)
        
        # Baseline
        baseline = {
            "error_rate": self.get_error_rate(),
            "throughput": self.get_throughput(),
            "circuit_state": self.get_circuit_breaker_state("redis")
        }
        print(f"Baseline - Error rate: {baseline['error_rate']:.2f}%, Throughput: {baseline['throughput']:.2f} req/s")
        
        # Inject failure
        print("\nInjecting failure: Scaling Redis to 0...")
        failure_time = time.time()
        subprocess.run(["kubectl", "scale", "statefulset", "redis", "--replicas=0", "-n", self.namespace])
        
        # Monitor
        metrics = []
        for i in range(60):
            time.sleep(1)
            elapsed = time.time() - failure_time
            
            metric = {
                "elapsed": elapsed,
                "error_rate": self.get_error_rate(),
                "throughput": self.get_throughput(),
                "circuit_state": self.get_circuit_breaker_state("redis")
            }
            metrics.append(metric)
            
            if i % 10 == 0:
                print(f"  [{elapsed:.0f}s] Error rate: {metric['error_rate']:.2f}%, Circuit: {metric['circuit_state']}")
        
        # Restore Redis
        print("\nRestoring Redis...")
        subprocess.run(["kubectl", "scale", "statefulset", "redis", "--replicas=1", "-n", self.namespace])
        time.sleep(30)  # Wait for Redis to be ready
        
        recovery = {
            "error_rate": self.get_error_rate(),
            "throughput": self.get_throughput(),
            "circuit_state": self.get_circuit_breaker_state("redis")
        }
        
        result = {
            "test": "redis_failure",
            "baseline": baseline,
            "during_failure": {
                "max_error_rate": max(m['error_rate'] for m in metrics),
                "circuit_opened": any(m['circuit_state'] == 2.0 for m in metrics)
            },
            "recovery": recovery,
            "metrics": metrics
        }
        
        print(f"\n=== Results ===")
        print(f"Max error rate: {result['during_failure']['max_error_rate']:.2f}%")
        print(f"Circuit opened: {result['during_failure']['circuit_opened']}")
        
        return result
    
    def test_retry_logic(self, duration: int = 120) -> Dict:
        """Test retry logic under network latency"""
        print("\n=== Testing Retry Logic ===")
        
        # Inject network latency using tc (traffic control)
        print("Injecting 200ms network latency to Stockfish...")
        pods = self.v1.list_namespaced_pod(
            self.namespace,
            label_selector="app=stockfish"
        )
        
        for pod in pods.items[:2]:  # Apply to first 2 pods
            cmd = ["kubectl", "exec", "-n", self.namespace, pod.metadata.name, "--", 
                   "tc", "qdisc", "add", "dev", "eth0", "root", "netem", "delay", "200ms"]
            subprocess.run(cmd, capture_output=True)
        
        # Get initial retry count
        initial_retries = self.get_retry_count()
        
        # Generate load
        print("Generating load (40 req/s for 2 minutes)...")
        load_cmd = "python3 load_generator.py --api-url http://localhost:30080 --pattern constant --rps 40 --duration 120 --output /tmp/retry_test_load.json &"
        subprocess.Popen(load_cmd, shell=True)
        
        time.sleep(duration)
        
        # Get final retry count
        final_retries = self.get_retry_count()
        retry_attempts = final_retries - initial_retries
        
        # Remove latency
        print("Removing network latency...")
        for pod in pods.items[:2]:
            cmd = ["kubectl", "exec", "-n", self.namespace, pod.metadata.name, "--",
                   "tc", "qdisc", "del", "dev", "eth0", "root"]
            subprocess.run(cmd, capture_output=True)
        
        # Load results
        time.sleep(5)
        with open('/tmp/retry_test_load.json', 'r') as f:
            load_results = json.load(f)
        
        total_requests = load_results['analysis']['total_requests']
        retry_rate = (retry_attempts / total_requests * 100) if total_requests > 0 else 0
        
        result = {
            "test": "retry_logic",
            "total_requests": total_requests,
            "retry_attempts": retry_attempts,
            "retry_rate_percent": retry_rate,
            "error_rate": load_results['analysis']['error_rate']
        }
        
        print(f"\n=== Results ===")
        print(f"Total requests: {total_requests}")
        print(f"Retry attempts: {retry_attempts}")
        print(f"Retry rate: {retry_rate:.2f}%")
        print(f"Error rate: {result['error_rate']:.2f}%")
        
        return result


def main():
    parser = argparse.ArgumentParser(description="Fault tolerance test suite")
    parser.add_argument("--namespace", default="stockfish", help="Kubernetes namespace")
    parser.add_argument("--prometheus-url", default="http://localhost:9090", help="Prometheus URL")
    parser.add_argument("--test", choices=["stockfish", "redis", "retry", "all"], default="all")
    parser.add_argument("--output", default="fault_tolerance_results.json", help="Output file")
    
    args = parser.parse_args()
    
    tester = FaultToleranceTester(args.namespace, args.prometheus_url)
    results = []
    
    if args.test in ["stockfish", "all"]:
        result = tester.test_stockfish_failure()
        results.append(result)
        time.sleep(60)  # Cool down between tests
    
    if args.test in ["redis", "all"]:
        result = tester.test_redis_failure()
        results.append(result)
        time.sleep(60)
    
    if args.test in ["retry", "all"]:
        result = tester.test_retry_logic()
        results.append(result)
    
    # Save results
    with open(args.output, 'w') as f:
        json.dump(results, f, indent=2)
    
    print(f"\n=== All Results Saved to {args.output} ===")


if __name__ == "__main__":
    main()
