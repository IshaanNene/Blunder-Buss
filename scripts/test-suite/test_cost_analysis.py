#!/usr/bin/env python3
"""
Cost Analysis Test Suite
Calculates actual infrastructure costs and compares deployment strategies
"""

import time
import json
import argparse
from typing import Dict, List
from datetime import datetime, timedelta
from kubernetes import client, config
from prometheus_api_client import PrometheusConnect
import pandas as pd
import matplotlib.pyplot as plt


# AWS EC2 pricing (us-east-1, on-demand and spot)
PRICING = {
    "m5.xlarge": {"on_demand": 0.192, "spot": 0.077},  # per hour
    "c5.2xlarge": {"on_demand": 0.340, "spot": 0.136},
    "storage_gb": 0.10,  # EBS gp3 per GB-month
    "data_transfer_gb": 0.09,  # per GB
    "load_balancer": 0.0225  # per hour
}


class CostAnalyzer:
    def __init__(self, namespace: str = "stockfish", prometheus_url: str = "http://localhost:9090"):
        config.load_kube_config()
        self.v1 = client.CoreV1Api()
        self.apps_v1 = client.AppsV1Api()
        self.namespace = namespace
        self.prom = PrometheusConnect(url=prometheus_url, disable_ssl=True)
    
    def get_replica_history(self, deployment: str, hours: int = 24) -> List[Dict]:
        """Get replica count history from Prometheus"""
        end_time = datetime.now()
        start_time = end_time - timedelta(hours=hours)
        
        query = f'kube_deployment_status_replicas{{namespace="{self.namespace}",deployment="{deployment}"}}'
        result = self.prom.custom_query_range(
            query=query,
            start_time=start_time,
            end_time=end_time,
            step='60s'
        )
        
        if not result:
            return []
        
        data = []
        for value in result[0]['values']:
            data.append({
                "timestamp": value[0],
                "replicas": int(float(value[1]))
            })
        
        return data
    
    def get_cpu_usage_history(self, deployment: str, hours: int = 24) -> List[Dict]:
        """Get CPU usage history"""
        end_time = datetime.now()
        start_time = end_time - timedelta(hours=hours)
        
        query = f'sum(rate(container_cpu_usage_seconds_total{{namespace="{self.namespace}",pod=~"{deployment}.*"}}[5m]))'
        result = self.prom.custom_query_range(
            query=query,
            start_time=start_time,
            end_time=end_time,
            step='60s'
        )
        
        if not result:
            return []
        
        data = []
        for value in result[0]['values']:
            data.append({
                "timestamp": value[0],
                "cpu_cores": float(value[1])
            })
        
        return data
    
    def get_request_count(self, hours: int = 24) -> int:
        """Get total request count"""
        query = f'sum(increase(api_requests_total[{hours}h]))'
        result = self.prom.custom_query(query=query)
        if result:
            return int(float(result[0]['value'][1]))
        return 0
    
    def calculate_static_cost(self, hours: int = 24) -> Dict:
        """Calculate cost for static deployment (no autoscaling)"""
        # Static deployment: 2 API, 10 workers, 12 Stockfish
        api_cost = 2 * PRICING["m5.xlarge"]["on_demand"] * hours
        worker_cost = 10 * PRICING["m5.xlarge"]["on_demand"] * hours
        stockfish_cost = 12 * PRICING["c5.2xlarge"]["on_demand"] * hours
        
        # Storage: Redis (50GB), Prometheus (100GB)
        storage_cost = (150 * PRICING["storage_gb"]) / 730 * hours  # 730 hours per month
        
        # Load balancer
        lb_cost = PRICING["load_balancer"] * hours
        
        total = api_cost + worker_cost + stockfish_cost + storage_cost + lb_cost
        
        return {
            "strategy": "static",
            "api_cost": api_cost,
            "worker_cost": worker_cost,
            "stockfish_cost": stockfish_cost,
            "storage_cost": storage_cost,
            "lb_cost": lb_cost,
            "total_cost": total,
            "hours": hours
        }
    
    def calculate_hpa_only_cost(self, hours: int = 24) -> Dict:
        """Calculate cost for HPA-only deployment"""
        # Get actual replica history
        worker_history = self.get_replica_history("worker", hours)
        stockfish_history = self.get_replica_history("stockfish", hours)
        
        # Calculate average replicas
        avg_workers = sum(h['replicas'] for h in worker_history) / len(worker_history) if worker_history else 5
        avg_stockfish = sum(h['replicas'] for h in stockfish_history) / len(stockfish_history) if stockfish_history else 6
        
        # HPA keeps minimum 2 API, scales workers and stockfish
        api_cost = 2 * PRICING["m5.xlarge"]["on_demand"] * hours
        worker_cost = avg_workers * PRICING["m5.xlarge"]["on_demand"] * hours
        stockfish_cost = avg_stockfish * PRICING["c5.2xlarge"]["on_demand"] * hours
        
        storage_cost = (150 * PRICING["storage_gb"]) / 730 * hours
        lb_cost = PRICING["load_balancer"] * hours
        
        total = api_cost + worker_cost + stockfish_cost + storage_cost + lb_cost
        
        return {
            "strategy": "hpa_only",
            "api_cost": api_cost,
            "worker_cost": worker_cost,
            "stockfish_cost": stockfish_cost,
            "storage_cost": storage_cost,
            "lb_cost": lb_cost,
            "total_cost": total,
            "avg_workers": avg_workers,
            "avg_stockfish": avg_stockfish,
            "hours": hours
        }
    
    def calculate_optimized_cost(self, hours: int = 24) -> Dict:
        """Calculate cost for optimized deployment (KEDA + HPA + spot instances)"""
        worker_history = self.get_replica_history("worker", hours)
        stockfish_history = self.get_replica_history("stockfish", hours)
        
        avg_workers = sum(h['replicas'] for h in worker_history) / len(worker_history) if worker_history else 3
        avg_stockfish = sum(h['replicas'] for h in stockfish_history) / len(stockfish_history) if stockfish_history else 4
        
        # API on-demand (for reliability), workers and stockfish on spot
        api_cost = 2 * PRICING["m5.xlarge"]["on_demand"] * hours
        worker_cost = avg_workers * PRICING["m5.xlarge"]["spot"] * hours
        stockfish_cost = avg_stockfish * PRICING["c5.2xlarge"]["spot"] * hours
        
        # Reduced storage (no Redis persistence)
        storage_cost = (100 * PRICING["storage_gb"]) / 730 * hours
        lb_cost = PRICING["load_balancer"] * hours
        
        total = api_cost + worker_cost + stockfish_cost + storage_cost + lb_cost
        
        return {
            "strategy": "optimized",
            "api_cost": api_cost,
            "worker_cost": worker_cost,
            "stockfish_cost": stockfish_cost,
            "storage_cost": storage_cost,
            "lb_cost": lb_cost,
            "total_cost": total,
            "avg_workers": avg_workers,
            "avg_stockfish": avg_stockfish,
            "hours": hours
        }
    
    def calculate_cost_per_request(self, total_cost: float, hours: int) -> float:
        """Calculate cost per 1M requests"""
        total_requests = self.get_request_count(hours)
        if total_requests == 0:
            return 0.0
        
        cost_per_million = (total_cost / total_requests) * 1_000_000
        return cost_per_million
    
    def calculate_efficiency_metrics(self, hours: int = 24) -> Dict:
        """Calculate cost efficiency metrics"""
        # Get CPU usage
        worker_cpu = self.get_cpu_usage_history("worker", hours)
        stockfish_cpu = self.get_cpu_usage_history("stockfish", hours)
        
        # Calculate average CPU usage
        avg_worker_cpu = sum(h['cpu_cores'] for h in worker_cpu) / len(worker_cpu) if worker_cpu else 0
        avg_stockfish_cpu = sum(h['cpu_cores'] for h in stockfish_cpu) / len(stockfish_cpu) if stockfish_cpu else 0
        
        # Get request count
        total_requests = self.get_request_count(hours)
        
        # Calculate operations per CPU-second
        total_cpu_seconds = (avg_worker_cpu + avg_stockfish_cpu) * hours * 3600
        ops_per_cpu_second = total_requests / total_cpu_seconds if total_cpu_seconds > 0 else 0
        
        # Get idle time from Prometheus
        query = f'sum(rate(worker_idle_time_seconds[{hours}h])) / sum(rate(worker_total_processing_seconds_count[{hours}h])) * 100'
        result = self.prom.custom_query(query=query)
        idle_percentage = float(result[0]['value'][1]) if result else 0
        
        return {
            "total_requests": total_requests,
            "avg_worker_cpu_cores": avg_worker_cpu,
            "avg_stockfish_cpu_cores": avg_stockfish_cpu,
            "total_cpu_seconds": total_cpu_seconds,
            "operations_per_cpu_second": ops_per_cpu_second,
            "worker_idle_percentage": idle_percentage
        }
    
    def compare_strategies(self, hours: int = 24) -> Dict:
        """Compare all deployment strategies"""
        print(f"\n=== Cost Analysis for {hours} hours ===\n")
        
        static = self.calculate_static_cost(hours)
        hpa_only = self.calculate_hpa_only_cost(hours)
        optimized = self.calculate_optimized_cost(hours)
        
        # Calculate cost per request
        static['cost_per_1m_requests'] = self.calculate_cost_per_request(static['total_cost'], hours)
        hpa_only['cost_per_1m_requests'] = self.calculate_cost_per_request(hpa_only['total_cost'], hours)
        optimized['cost_per_1m_requests'] = self.calculate_cost_per_request(optimized['total_cost'], hours)
        
        # Calculate savings
        hpa_only['savings_vs_static_percent'] = (1 - hpa_only['total_cost'] / static['total_cost']) * 100
        optimized['savings_vs_static_percent'] = (1 - optimized['total_cost'] / static['total_cost']) * 100
        optimized['savings_vs_hpa_percent'] = (1 - optimized['total_cost'] / hpa_only['total_cost']) * 100
        
        # Get efficiency metrics
        efficiency = self.calculate_efficiency_metrics(hours)
        
        # Print results
        print("Strategy: Static (No Autoscaling)")
        print(f"  Total Cost: ${static['total_cost']:.2f}")
        print(f"  Cost per 1M requests: ${static['cost_per_1m_requests']:.2f}")
        print()
        
        print("Strategy: HPA Only")
        print(f"  Total Cost: ${hpa_only['total_cost']:.2f}")
        print(f"  Cost per 1M requests: ${hpa_only['cost_per_1m_requests']:.2f}")
        print(f"  Savings vs Static: {hpa_only['savings_vs_static_percent']:.1f}%")
        print(f"  Avg Workers: {hpa_only['avg_workers']:.1f}")
        print(f"  Avg Stockfish: {hpa_only['avg_stockfish']:.1f}")
        print()
        
        print("Strategy: Optimized (KEDA + HPA + Spot)")
        print(f"  Total Cost: ${optimized['total_cost']:.2f}")
        print(f"  Cost per 1M requests: ${optimized['cost_per_1m_requests']:.2f}")
        print(f"  Savings vs Static: {optimized['savings_vs_static_percent']:.1f}%")
        print(f"  Savings vs HPA Only: {optimized['savings_vs_hpa_percent']:.1f}%")
        print(f"  Avg Workers: {optimized['avg_workers']:.1f}")
        print(f"  Avg Stockfish: {optimized['avg_stockfish']:.1f}")
        print()
        
        print("Efficiency Metrics:")
        print(f"  Total Requests: {efficiency['total_requests']:,}")
        print(f"  Operations per CPU-second: {efficiency['operations_per_cpu_second']:.3f}")
        print(f"  Worker Idle Time: {efficiency['worker_idle_percentage']:.1f}%")
        
        return {
            "static": static,
            "hpa_only": hpa_only,
            "optimized": optimized,
            "efficiency": efficiency
        }
    
    def plot_cost_comparison(self, results: Dict, output_file: str):
        """Generate cost comparison plots"""
        fig, axes = plt.subplots(2, 2, figsize=(14, 10))
        
        strategies = ['Static', 'HPA Only', 'Optimized']
        costs = [
            results['static']['total_cost'],
            results['hpa_only']['total_cost'],
            results['optimized']['total_cost']
        ]
        cost_per_req = [
            results['static']['cost_per_1m_requests'],
            results['hpa_only']['cost_per_1m_requests'],
            results['optimized']['cost_per_1m_requests']
        ]
        
        # Total cost comparison
        axes[0, 0].bar(strategies, costs, color=['red', 'orange', 'green'])
        axes[0, 0].set_ylabel('Total Cost ($)')
        axes[0, 0].set_title('Total Infrastructure Cost')
        axes[0, 0].grid(True, axis='y')
        
        # Cost per request
        axes[0, 1].bar(strategies, cost_per_req, color=['red', 'orange', 'green'])
        axes[0, 1].set_ylabel('Cost per 1M Requests ($)')
        axes[0, 1].set_title('Cost Efficiency')
        axes[0, 1].grid(True, axis='y')
        
        # Cost breakdown for optimized strategy
        breakdown = [
            results['optimized']['api_cost'],
            results['optimized']['worker_cost'],
            results['optimized']['stockfish_cost'],
            results['optimized']['storage_cost'],
            results['optimized']['lb_cost']
        ]
        labels = ['API', 'Worker', 'Stockfish', 'Storage', 'Load Balancer']
        axes[1, 0].pie(breakdown, labels=labels, autopct='%1.1f%%')
        axes[1, 0].set_title('Optimized Strategy Cost Breakdown')
        
        # Savings comparison
        savings = [
            0,
            results['hpa_only']['savings_vs_static_percent'],
            results['optimized']['savings_vs_static_percent']
        ]
        axes[1, 1].bar(strategies, savings, color=['red', 'orange', 'green'])
        axes[1, 1].set_ylabel('Savings vs Static (%)')
        axes[1, 1].set_title('Cost Savings')
        axes[1, 1].grid(True, axis='y')
        
        plt.tight_layout()
        plt.savefig(output_file, dpi=300)
        print(f"\nPlot saved to {output_file}")


def main():
    parser = argparse.ArgumentParser(description="Cost analysis test suite")
    parser.add_argument("--namespace", default="stockfish", help="Kubernetes namespace")
    parser.add_argument("--prometheus-url", default="http://localhost:9090", help="Prometheus URL")
    parser.add_argument("--hours", type=int, default=24, help="Analysis period in hours")
    parser.add_argument("--output", default="cost_analysis_results.json", help="Output file")
    
    args = parser.parse_args()
    
    analyzer = CostAnalyzer(args.namespace, args.prometheus_url)
    results = analyzer.compare_strategies(args.hours)
    
    # Save results
    with open(args.output, 'w') as f:
        json.dump(results, f, indent=2)
    
    print(f"\n=== Results Saved to {args.output} ===")
    
    # Generate plots
    plot_file = args.output.replace('.json', '.png')
    analyzer.plot_cost_comparison(results, plot_file)


if __name__ == "__main__":
    main()
