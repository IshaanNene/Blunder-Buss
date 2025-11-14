#!/usr/bin/env python3
"""
Generate Summary Report
Consolidates all test results into a single summary for the IEEE paper
"""

import json
import os
import argparse
from typing import Dict
from tabulate import tabulate
from colorama import Fore, Style, init

init(autoreset=True)


def load_json_file(filepath: str) -> Dict:
    """Load JSON file safely"""
    try:
        with open(filepath, 'r') as f:
            return json.load(f)
    except Exception as e:
        print(f"Warning: Could not load {filepath}: {e}")
        return {}


def summarize_load_tests(results_dir: str) -> Dict:
    """Summarize load test results"""
    workloads = {
        "A (Light - 10 req/s)": "workload_a_light.json",
        "B (Medium - 50 req/s)": "workload_b_medium.json",
        "C (Heavy - 100 req/s)": "workload_c_heavy.json",
        "D (Variable)": "workload_d_variable.json",
        "E (Spike)": "workload_e_spike.json"
    }
    
    summary = {}
    
    for name, filename in workloads.items():
        filepath = os.path.join(results_dir, filename)
        data = load_json_file(filepath)
        
        if 'analysis' in data:
            analysis = data['analysis']
            summary[name] = {
                "total_requests": analysis.get('total_requests', 0),
                "error_rate_percent": analysis.get('error_rate', 0),
                "throughput_rps": analysis.get('throughput_rps', 0),
                "p50_latency_ms": analysis.get('latency', {}).get('p50', 0),
                "p95_latency_ms": analysis.get('latency', {}).get('p95', 0),
                "p99_latency_ms": analysis.get('latency', {}).get('p99', 0)
            }
    
    return summary


def summarize_autoscaling(results_dir: str) -> Dict:
    """Summarize autoscaling test results"""
    filepath = os.path.join(results_dir, "autoscaling_results.json")
    data = load_json_file(filepath)
    
    if not data:
        return {}
    
    summary = {}
    
    for test in data:
        test_name = test.get('test', 'unknown')
        
        if test_name == 'worker_scale_up':
            summary['worker_scale_up'] = {
                "initial_replicas": test.get('initial_replicas', 0),
                "final_replicas": test.get('final_replicas', 0),
                "max_replicas": test.get('max_replicas', 0),
                "scale_events": len(test.get('scale_events', [])),
                "avg_scale_time_seconds": test.get('avg_scale_time_seconds', 0)
            }
        elif test_name == 'worker_scale_down':
            summary['worker_scale_down'] = {
                "initial_replicas": test.get('initial_replicas', 0),
                "final_replicas": test.get('final_replicas', 0),
                "scale_events": len(test.get('scale_events', []))
            }
        elif test_name == 'stockfish_hpa_scaling':
            summary['stockfish_scaling'] = {
                "initial_replicas": test.get('initial_replicas', 0),
                "final_replicas": test.get('final_replicas', 0),
                "max_cpu_utilization": test.get('max_cpu_utilization', 0)
            }
    
    return summary


def summarize_fault_tolerance(results_dir: str) -> Dict:
    """Summarize fault tolerance test results"""
    filepath = os.path.join(results_dir, "fault_tolerance_results.json")
    data = load_json_file(filepath)
    
    if not data:
        return {}
    
    summary = {}
    
    for test in data:
        test_name = test.get('test', 'unknown')
        
        if test_name == 'stockfish_failure':
            during = test.get('during_failure', {})
            baseline = test.get('baseline', {})
            
            throughput_maintained = 0
            if baseline.get('throughput', 0) > 0:
                throughput_maintained = (during.get('min_throughput', 0) / baseline['throughput']) * 100
            
            summary['stockfish_failure'] = {
                "baseline_error_rate": baseline.get('error_rate', 0),
                "max_error_rate": during.get('max_error_rate', 0),
                "circuit_opened": during.get('circuit_opened', False),
                "recovery_time_seconds": test.get('recovery_time_seconds', 0),
                "throughput_maintained_percent": throughput_maintained
            }
        elif test_name == 'redis_failure':
            summary['redis_failure'] = {
                "max_error_rate": test.get('during_failure', {}).get('max_error_rate', 0),
                "circuit_opened": test.get('during_failure', {}).get('circuit_opened', False)
            }
        elif test_name == 'retry_logic':
            summary['retry_logic'] = {
                "total_requests": test.get('total_requests', 0),
                "retry_attempts": test.get('retry_attempts', 0),
                "retry_rate_percent": test.get('retry_rate_percent', 0),
                "error_rate": test.get('error_rate', 0)
            }
    
    return summary


def summarize_cost_analysis(results_dir: str) -> Dict:
    """Summarize cost analysis results"""
    filepath = os.path.join(results_dir, "cost_analysis_results.json")
    data = load_json_file(filepath)
    
    if not data:
        return {}
    
    summary = {
        "static": {
            "total_cost": data.get('static', {}).get('total_cost', 0),
            "cost_per_1m_requests": data.get('static', {}).get('cost_per_1m_requests', 0)
        },
        "hpa_only": {
            "total_cost": data.get('hpa_only', {}).get('total_cost', 0),
            "cost_per_1m_requests": data.get('hpa_only', {}).get('cost_per_1m_requests', 0),
            "savings_percent": data.get('hpa_only', {}).get('savings_vs_static_percent', 0)
        },
        "optimized": {
            "total_cost": data.get('optimized', {}).get('total_cost', 0),
            "cost_per_1m_requests": data.get('optimized', {}).get('cost_per_1m_requests', 0),
            "savings_vs_static_percent": data.get('optimized', {}).get('savings_vs_static_percent', 0),
            "savings_vs_hpa_percent": data.get('optimized', {}).get('savings_vs_hpa_percent', 0)
        },
        "efficiency": data.get('efficiency', {})
    }
    
    return summary


def summarize_observability(results_dir: str) -> Dict:
    """Summarize observability overhead results"""
    filepath = os.path.join(results_dir, "observability_overhead_results.json")
    data = load_json_file(filepath)
    
    if not data:
        return {}
    
    return {
        "total_overhead_ms": data.get('observability_overhead', {}).get('total_overhead_ms', {}),
        "overhead_percentage": data.get('overhead_percentage', 0),
        "meets_paper_claim": data.get('analysis', {}).get('meets_paper_claim', False)
    }


def print_summary(summary: Dict):
    """Print formatted summary"""
    print("\n" + "="*80)
    print(Fore.CYAN + Style.BRIGHT + "BLUNDER-BUSS TEST SUITE SUMMARY")
    print("="*80 + Style.RESET_ALL)
    
    # Load Tests
    if 'load_tests' in summary:
        print("\n" + Fore.YELLOW + "LOAD TEST RESULTS" + Style.RESET_ALL)
        table_data = []
        for workload, data in summary['load_tests'].items():
            table_data.append([
                workload,
                f"{data['p50_latency_ms']:.2f}",
                f"{data['p95_latency_ms']:.2f}",
                f"{data['p99_latency_ms']:.2f}",
                f"{data['error_rate_percent']:.2f}%",
                f"{data['throughput_rps']:.1f}"
            ])
        
        print(tabulate(table_data, 
                      headers=["Workload", "P50 (ms)", "P95 (ms)", "P99 (ms)", "Error Rate", "Throughput"],
                      tablefmt="grid"))
    
    # Autoscaling
    if 'autoscaling' in summary:
        print("\n" + Fore.YELLOW + "AUTOSCALING RESULTS" + Style.RESET_ALL)
        auto = summary['autoscaling']
        
        if 'worker_scale_up' in auto:
            print(f"\nWorker Scale-Up:")
            print(f"  Initial → Final Replicas: {auto['worker_scale_up']['initial_replicas']} → {auto['worker_scale_up']['final_replicas']}")
            print(f"  Average Scale Time: {auto['worker_scale_up']['avg_scale_time_seconds']:.1f}s")
            print(f"  Scale Events: {auto['worker_scale_up']['scale_events']}")
        
        if 'stockfish_scaling' in auto:
            print(f"\nStockfish HPA Scaling:")
            print(f"  Initial → Final Replicas: {auto['stockfish_scaling']['initial_replicas']} → {auto['stockfish_scaling']['final_replicas']}")
            print(f"  Max CPU Utilization: {auto['stockfish_scaling']['max_cpu_utilization']:.1f}%")
    
    # Fault Tolerance
    if 'fault_tolerance' in summary:
        print("\n" + Fore.YELLOW + "FAULT TOLERANCE RESULTS" + Style.RESET_ALL)
        ft = summary['fault_tolerance']
        
        if 'stockfish_failure' in ft:
            sf = ft['stockfish_failure']
            print(f"\nStockfish Failure Test (50% pods deleted):")
            print(f"  Baseline Error Rate: {sf['baseline_error_rate']:.2f}%")
            print(f"  Max Error Rate During Failure: {sf['max_error_rate']:.2f}%")
            print(f"  Circuit Breaker Opened: {Fore.GREEN if sf['circuit_opened'] else Fore.RED}{sf['circuit_opened']}{Style.RESET_ALL}")
            print(f"  Recovery Time: {sf['recovery_time_seconds']:.1f}s")
            print(f"  Throughput Maintained: {sf['throughput_maintained_percent']:.1f}%")
        
        if 'retry_logic' in ft:
            rl = ft['retry_logic']
            print(f"\nRetry Logic Test:")
            print(f"  Total Requests: {rl['total_requests']}")
            print(f"  Retry Attempts: {rl['retry_attempts']}")
            print(f"  Retry Rate: {rl['retry_rate_percent']:.2f}%")
            print(f"  Final Error Rate: {rl['error_rate']:.2f}%")
    
    # Cost Analysis
    if 'cost_analysis' in summary:
        print("\n" + Fore.YELLOW + "COST ANALYSIS RESULTS" + Style.RESET_ALL)
        cost = summary['cost_analysis']
        
        table_data = [
            ["Static (No Autoscaling)", 
             f"${cost['static']['total_cost']:.2f}",
             f"${cost['static']['cost_per_1m_requests']:.2f}",
             "0%"],
            ["HPA Only",
             f"${cost['hpa_only']['total_cost']:.2f}",
             f"${cost['hpa_only']['cost_per_1m_requests']:.2f}",
             f"{cost['hpa_only']['savings_percent']:.1f}%"],
            ["Optimized (KEDA+HPA+Spot)",
             f"${cost['optimized']['total_cost']:.2f}",
             f"${cost['optimized']['cost_per_1m_requests']:.2f}",
             f"{cost['optimized']['savings_vs_static_percent']:.1f}%"]
        ]
        
        print(tabulate(table_data,
                      headers=["Strategy", "Total Cost", "Cost/1M Req", "Savings"],
                      tablefmt="grid"))
        
        if 'efficiency' in cost:
            eff = cost['efficiency']
            print(f"\nEfficiency Metrics:")
            print(f"  Operations per CPU-second: {eff.get('operations_per_cpu_second', 0):.3f}")
            print(f"  Worker Idle Time: {eff.get('worker_idle_percentage', 0):.1f}%")
    
    # Observability Overhead
    if 'observability' in summary:
        print("\n" + Fore.YELLOW + "OBSERVABILITY OVERHEAD" + Style.RESET_ALL)
        obs = summary['observability']
        overhead = obs.get('total_overhead_ms', {})
        
        print(f"  Mean Overhead: {overhead.get('mean', 0):.3f}ms")
        print(f"  P95 Overhead: {overhead.get('p95', 0):.3f}ms")
        print(f"  P99 Overhead: {overhead.get('p99', 0):.3f}ms")
        print(f"  Overhead Percentage: {obs.get('overhead_percentage', 0):.2f}%")
        print(f"  Meets Paper Claim (<2ms): {Fore.GREEN if obs.get('meets_paper_claim') else Fore.RED}{obs.get('meets_paper_claim')}{Style.RESET_ALL}")
    
    print("\n" + "="*80)
    print(Fore.GREEN + Style.BRIGHT + "TEST SUITE COMPLETE")
    print("="*80 + Style.RESET_ALL + "\n")


def main():
    parser = argparse.ArgumentParser(description="Generate summary report")
    parser.add_argument("--results-dir", required=True, help="Results directory")
    parser.add_argument("--output", default="summary_report.json", help="Output file")
    
    args = parser.parse_args()
    
    # Collect all summaries
    summary = {
        "load_tests": summarize_load_tests(args.results_dir),
        "autoscaling": summarize_autoscaling(args.results_dir),
        "fault_tolerance": summarize_fault_tolerance(args.results_dir),
        "cost_analysis": summarize_cost_analysis(args.results_dir),
        "observability": summarize_observability(args.results_dir)
    }
    
    # Print summary
    print_summary(summary)
    
    # Save summary
    output_path = os.path.join(args.results_dir, os.path.basename(args.output))
    with open(output_path, 'w') as f:
        json.dump(summary, f, indent=2)
    
    print(f"Summary saved to: {output_path}")


if __name__ == "__main__":
    main()
