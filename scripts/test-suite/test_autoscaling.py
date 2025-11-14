#!/usr/bin/env python3
"""
Autoscaling Test Suite
Tests KEDA and HPA scaling behavior and measures response times
"""

import time
import json
import subprocess
from datetime import datetime
from typing import Dict, List, Tuple
import argparse
from kubernetes import client, config
from prometheus_api_client import PrometheusConnect
import matplotlib.pyplot as plt
import pandas as pd


class AutoscalingTester:
    def __init__(self, namespace: str = "stockfish", prometheus_url: str = "http://localhost:9090"):
        config.load_kube_config()
        self.v1 = client.CoreV1Api()
        self.apps_v1 = client.AppsV1Api()
        self.namespace = namespace
        self.prom = PrometheusConnect(url=prometheus_url, disable_ssl=True)
    
    def get_replica_count(self, deployment: str) -> int:
        """Get current replica count for a deployment"""
        try:
            dep = self.apps_v1.read_namespaced_deployment(deployment, self.namespace)
            return dep.status.replicas or 0
        except Exception as e:
            print(f"Error getting replica count: {e}")
            return 0
    
    def get_queue_depth(self) -> int:
        """Get current Redis queue depth from Prometheus"""
        query = 'redis_queue_depth{namespace="stockfish"}'
        result = self.prom.custom_query(query=query)
        if result:
            return int(float(result[0]['value'][1]))
        return 0
    
    def get_cpu_utilization(self, deployment: str) -> float:
        """Get average CPU utilization for a deployment"""
        query = f'avg(rate(container_cpu_usage_seconds_total{{namespace="{self.namespace}",pod=~"{deployment}.*"}}[1m])) * 100'
        result = self.prom.custom_query(query=query)
        if result:
            return float(result[0]['value'][1])
        return 0.0
    
    def monitor_scaling(self, deployment: str, duration_seconds: int, interval: int = 5) -> List[Dict]:
        """Monitor scaling metrics over time"""
        print(f"Monitoring {deployment} for {duration_seconds} seconds...")
        
        metrics = []
        start_time = time.time()
        end_time = start_time + duration_seconds
        
        while time.time() < end_time:
            timestamp = time.time()
            replicas = self.get_replica_count(deployment)
            queue_depth = self.get_queue_depth()
            cpu = self.get_cpu_utilization(deployment)
            
            metric = {
                "timestamp": timestamp,
                "elapsed": timestamp - start_time,
                "replicas": replicas,
                "queue_depth": queue_depth,
                "cpu_utilization": cpu
            }
            
            metrics.append(metric)
            print(f"  [{metric['elapsed']:.0f}s] Replicas: {replicas}, Queue: {queue_depth}, CPU: {cpu:.1f}%")
            
            time.sleep(interval)
        
        return metrics
    
    def test_worker_scale_up(self, target_rps: int, duration: int = 600) -> Dict:
        """Test worker scaling in response to load"""
        print("\n=== Testing Worker Scale-Up ===")
        print(f"Target: {target_rps} req/s for {duration}s")
        
        # Get initial state
        initial_replicas = self.get_replica_count("worker")
        print(f"Initial worker replicas: {initial_replicas}")
        
        # Start load generation in background
        print("Starting load generation...")
        load_cmd = f"python3 load_generator.py --api-url http://localhost:30080 --pattern constant --rps {target_rps} --duration {duration} --output /tmp/scale_up_load.json &"
        subprocess.Popen(load_cmd, shell=True)
        
        # Monitor scaling
        time.sleep(5)  # Wait for load to start
        metrics = self.monitor_scaling("worker", duration)
        
        # Analyze results
        final_replicas = self.get_replica_count("worker")
        max_replicas = max(m['replicas'] for m in metrics)
        
        # Find time to scale
        scale_events = []
        prev_replicas = initial_replicas
        for m in metrics:
            if m['replicas'] > prev_replicas:
                scale_events.append({
                    "time": m['elapsed'],
                    "from": prev_replicas,
                    "to": m['replicas']
                })
                prev_replicas = m['replicas']
        
        avg_scale_time = sum(e['time'] for e in scale_events) / len(scale_events) if scale_events else 0
        
        result = {
            "test": "worker_scale_up",
            "initial_replicas": initial_replicas,
            "final_replicas": final_replicas,
            "max_replicas": max_replicas,
            "scale_events": scale_events,
            "avg_scale_time_seconds": avg_scale_time,
            "metrics": metrics
        }
        
        print(f"\nResults:")
        print(f"  Initial replicas: {initial_replicas}")
        print(f"  Final replicas: {final_replicas}")
        print(f"  Max replicas: {max_replicas}")
        print(f"  Scale events: {len(scale_events)}")
        print(f"  Avg scale time: {avg_scale_time:.1f}s")
        
        return result
    
    def test_worker_scale_down(self, duration: int = 900) -> Dict:
        """Test worker scaling down after load decreases"""
        print("\n=== Testing Worker Scale-Down ===")
        
        initial_replicas = self.get_replica_count("worker")
        print(f"Initial worker replicas: {initial_replicas}")
        print("Waiting for scale-down (this takes ~5-10 minutes)...")
        
        # Monitor scaling
        metrics = self.monitor_scaling("worker", duration)
        
        final_replicas = self.get_replica_count("worker")
        
        # Find scale-down events
        scale_events = []
        prev_replicas = initial_replicas
        for m in metrics:
            if m['replicas'] < prev_replicas:
                scale_events.append({
                    "time": m['elapsed'],
                    "from": prev_replicas,
                    "to": m['replicas']
                })
                prev_replicas = m['replicas']
        
        result = {
            "test": "worker_scale_down",
            "initial_replicas": initial_replicas,
            "final_replicas": final_replicas,
            "scale_events": scale_events,
            "metrics": metrics
        }
        
        print(f"\nResults:")
        print(f"  Initial replicas: {initial_replicas}")
        print(f"  Final replicas: {final_replicas}")
        print(f"  Scale-down events: {len(scale_events)}")
        
        return result
    
    def test_stockfish_scaling(self, duration: int = 600) -> Dict:
        """Test Stockfish HPA scaling based on CPU"""
        print("\n=== Testing Stockfish HPA Scaling ===")
        
        initial_replicas = self.get_replica_count("stockfish")
        print(f"Initial Stockfish replicas: {initial_replicas}")
        
        # Generate high load to trigger CPU-based scaling
        print("Generating high load (100 req/s)...")
        load_cmd = "python3 load_generator.py --api-url http://localhost:30080 --pattern constant --rps 100 --duration 600 --output /tmp/stockfish_scale.json &"
        subprocess.Popen(load_cmd, shell=True)
        
        time.sleep(5)
        metrics = self.monitor_scaling("stockfish", duration)
        
        final_replicas = self.get_replica_count("stockfish")
        max_replicas = max(m['replicas'] for m in metrics)
        max_cpu = max(m['cpu_utilization'] for m in metrics)
        
        result = {
            "test": "stockfish_hpa_scaling",
            "initial_replicas": initial_replicas,
            "final_replicas": final_replicas,
            "max_replicas": max_replicas,
            "max_cpu_utilization": max_cpu,
            "metrics": metrics
        }
        
        print(f"\nResults:")
        print(f"  Initial replicas: {initial_replicas}")
        print(f"  Final replicas: {final_replicas}")
        print(f"  Max CPU: {max_cpu:.1f}%")
        
        return result
    
    def plot_scaling_results(self, results: List[Dict], output_file: str):
        """Generate plots for scaling results"""
        fig, axes = plt.subplots(3, 1, figsize=(12, 10))
        
        for result in results:
            if 'metrics' not in result:
                continue
            
            df = pd.DataFrame(result['metrics'])
            test_name = result['test']
            
            # Plot replicas
            axes[0].plot(df['elapsed'] / 60, df['replicas'], label=test_name, marker='o')
            axes[0].set_ylabel('Replica Count')
            axes[0].set_title('Replica Count Over Time')
            axes[0].legend()
            axes[0].grid(True)
            
            # Plot queue depth
            axes[1].plot(df['elapsed'] / 60, df['queue_depth'], label=test_name, marker='o')
            axes[1].set_ylabel('Queue Depth')
            axes[1].set_title('Redis Queue Depth Over Time')
            axes[1].legend()
            axes[1].grid(True)
            
            # Plot CPU
            axes[2].plot(df['elapsed'] / 60, df['cpu_utilization'], label=test_name, marker='o')
            axes[2].set_ylabel('CPU Utilization (%)')
            axes[2].set_xlabel('Time (minutes)')
            axes[2].set_title('CPU Utilization Over Time')
            axes[2].legend()
            axes[2].grid(True)
        
        plt.tight_layout()
        plt.savefig(output_file, dpi=300)
        print(f"\nPlot saved to {output_file}")


def main():
    parser = argparse.ArgumentParser(description="Autoscaling test suite")
    parser.add_argument("--namespace", default="stockfish", help="Kubernetes namespace")
    parser.add_argument("--prometheus-url", default="http://localhost:9090", help="Prometheus URL")
    parser.add_argument("--test", choices=["scale-up", "scale-down", "stockfish", "all"], default="all")
    parser.add_argument("--output", default="autoscaling_results.json", help="Output file")
    
    args = parser.parse_args()
    
    tester = AutoscalingTester(args.namespace, args.prometheus_url)
    results = []
    
    if args.test in ["scale-up", "all"]:
        result = tester.test_worker_scale_up(target_rps=50, duration=600)
        results.append(result)
    
    if args.test in ["scale-down", "all"]:
        result = tester.test_worker_scale_down(duration=900)
        results.append(result)
    
    if args.test in ["stockfish", "all"]:
        result = tester.test_stockfish_scaling(duration=600)
        results.append(result)
    
    # Save results
    with open(args.output, 'w') as f:
        json.dump(results, f, indent=2)
    
    print(f"\n=== All Results Saved to {args.output} ===")
    
    # Generate plots
    plot_file = args.output.replace('.json', '.png')
    tester.plot_scaling_results(results, plot_file)


if __name__ == "__main__":
    main()
