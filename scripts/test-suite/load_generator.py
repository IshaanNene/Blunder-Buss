#!/usr/bin/env python3
"""
Load Generator for Blunder-Buss
Generates chess analysis requests with configurable load patterns
"""

import requests
import time
import json
import threading
import queue
from dataclasses import dataclass, asdict
from typing import List, Dict, Optional
import numpy as np
from datetime import datetime
import argparse


@dataclass
class RequestResult:
    timestamp: float
    duration_ms: float
    status_code: int
    correlation_id: str
    error: Optional[str] = None


class LoadGenerator:
    def __init__(self, api_url: str, results_queue: queue.Queue):
        self.api_url = api_url
        self.results_queue = results_queue
        self.session = requests.Session()
        
        # Sample chess positions (various complexities)
        self.positions = [
            # Opening position
            "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1",
            # Mid-game
            "r1bqkb1r/pppp1ppp/2n2n2/4p3/2B1P3/5N2/PPPP1PPP/RNBQK2R w KQkq - 4 4",
            # Complex position
            "r1bq1rk1/ppp2ppp/2np1n2/2b1p3/2B1P3/2NP1N2/PPP2PPP/R1BQ1RK1 w - - 0 8",
            # Endgame
            "8/5k2/3p4/1p1Pp3/pP2Pp2/P4P2/8/6K1 w - - 0 1",
        ]
    
    def generate_request(self, elo: int = 1600, max_time_ms: int = 1000) -> Dict:
        """Generate a single chess analysis request"""
        return {
            "fen": np.random.choice(self.positions),
            "elo": elo,
            "max_time_ms": max_time_ms
        }
    
    def send_request(self, elo: int = 1600, max_time_ms: int = 1000):
        """Send a single request and record results"""
        payload = self.generate_request(elo, max_time_ms)
        start_time = time.time()
        
        try:
            response = self.session.post(
                f"{self.api_url}/move",
                json=payload,
                timeout=30
            )
            
            duration_ms = (time.time() - start_time) * 1000
            correlation_id = response.headers.get('X-Correlation-ID', 'unknown')
            
            result = RequestResult(
                timestamp=start_time,
                duration_ms=duration_ms,
                status_code=response.status_code,
                correlation_id=correlation_id,
                error=None if response.ok else response.text
            )
        except Exception as e:
            duration_ms = (time.time() - start_time) * 1000
            result = RequestResult(
                timestamp=start_time,
                duration_ms=duration_ms,
                status_code=0,
                correlation_id='error',
                error=str(e)
            )
        
        self.results_queue.put(result)
    
    def worker_thread(self, requests_per_second: float, duration_seconds: int, 
                     elo: int, max_time_ms: int):
        """Worker thread that sends requests at specified rate"""
        interval = 1.0 / requests_per_second
        end_time = time.time() + duration_seconds
        
        while time.time() < end_time:
            self.send_request(elo, max_time_ms)
            time.sleep(interval)


def run_constant_load(api_url: str, rps: float, duration: int, 
                     elo: int = 1600, max_time_ms: int = 1000) -> List[RequestResult]:
    """Run constant load test"""
    print(f"Running constant load: {rps} req/s for {duration}s")
    
    results_queue = queue.Queue()
    generator = LoadGenerator(api_url, results_queue)
    
    # Start worker threads (one per RPS to maintain rate)
    threads = []
    num_threads = max(1, int(rps))
    rps_per_thread = rps / num_threads
    
    for _ in range(num_threads):
        thread = threading.Thread(
            target=generator.worker_thread,
            args=(rps_per_thread, duration, elo, max_time_ms)
        )
        thread.start()
        threads.append(thread)
    
    # Wait for all threads to complete
    for thread in threads:
        thread.join()
    
    # Collect results
    results = []
    while not results_queue.empty():
        results.append(results_queue.get())
    
    return results


def run_ramp_load(api_url: str, start_rps: float, end_rps: float, 
                 duration: int, elo: int = 1600, max_time_ms: int = 1000) -> List[RequestResult]:
    """Run ramping load test"""
    print(f"Running ramp load: {start_rps} -> {end_rps} req/s over {duration}s")
    
    results_queue = queue.Queue()
    generator = LoadGenerator(api_url, results_queue)
    
    start_time = time.time()
    end_time = start_time + duration
    
    threads = []
    
    while time.time() < end_time:
        elapsed = time.time() - start_time
        progress = elapsed / duration
        current_rps = start_rps + (end_rps - start_rps) * progress
        
        # Send requests for 1 second at current rate
        thread = threading.Thread(
            target=generator.worker_thread,
            args=(current_rps, 1, elo, max_time_ms)
        )
        thread.start()
        threads.append(thread)
        
        time.sleep(1)
    
    # Wait for all threads
    for thread in threads:
        thread.join()
    
    # Collect results
    results = []
    while not results_queue.empty():
        results.append(results_queue.get())
    
    return results


def run_spike_load(api_url: str, base_rps: float, spike_rps: float,
                  spike_duration: int, spike_interval: int, total_duration: int,
                  elo: int = 1600, max_time_ms: int = 1000) -> List[RequestResult]:
    """Run spike load test"""
    print(f"Running spike load: {base_rps} req/s with spikes to {spike_rps} req/s")
    
    results_queue = queue.Queue()
    generator = LoadGenerator(api_url, results_queue)
    
    start_time = time.time()
    end_time = start_time + total_duration
    next_spike = start_time + spike_interval
    
    threads = []
    
    while time.time() < end_time:
        current_time = time.time()
        
        # Determine if we're in a spike
        if current_time >= next_spike and current_time < next_spike + spike_duration:
            current_rps = spike_rps
        else:
            current_rps = base_rps
            if current_time >= next_spike + spike_duration:
                next_spike = current_time + spike_interval
        
        # Send requests for 1 second
        thread = threading.Thread(
            target=generator.worker_thread,
            args=(current_rps, 1, elo, max_time_ms)
        )
        thread.start()
        threads.append(thread)
        
        time.sleep(1)
    
    # Wait for all threads
    for thread in threads:
        thread.join()
    
    # Collect results
    results = []
    while not results_queue.empty():
        results.append(results_queue.get())
    
    return results


def calculate_percentiles(durations: List[float]) -> Dict[str, float]:
    """Calculate latency percentiles"""
    if not durations:
        return {"p50": 0, "p95": 0, "p99": 0, "mean": 0, "min": 0, "max": 0}
    
    return {
        "p50": np.percentile(durations, 50),
        "p95": np.percentile(durations, 95),
        "p99": np.percentile(durations, 99),
        "mean": np.mean(durations),
        "min": np.min(durations),
        "max": np.max(durations)
    }


def analyze_results(results: List[RequestResult]) -> Dict:
    """Analyze test results"""
    if not results:
        return {}
    
    durations = [r.duration_ms for r in results]
    successful = [r for r in results if r.status_code == 200]
    errors = [r for r in results if r.status_code != 200]
    
    total_duration = max(r.timestamp for r in results) - min(r.timestamp for r in results)
    
    analysis = {
        "total_requests": len(results),
        "successful_requests": len(successful),
        "failed_requests": len(errors),
        "error_rate": len(errors) / len(results) * 100,
        "throughput_rps": len(results) / total_duration if total_duration > 0 else 0,
        "latency": calculate_percentiles(durations),
        "test_duration_seconds": total_duration
    }
    
    return analysis


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Load generator for Blunder-Buss")
    parser.add_argument("--api-url", required=True, help="API URL (e.g., http://localhost:30080)")
    parser.add_argument("--pattern", choices=["constant", "ramp", "spike"], required=True)
    parser.add_argument("--rps", type=float, default=10, help="Requests per second")
    parser.add_argument("--duration", type=int, default=60, help="Test duration in seconds")
    parser.add_argument("--output", default="results.json", help="Output file")
    
    args = parser.parse_args()
    
    if args.pattern == "constant":
        results = run_constant_load(args.api_url, args.rps, args.duration)
    elif args.pattern == "ramp":
        results = run_ramp_load(args.api_url, 10, args.rps, args.duration)
    elif args.pattern == "spike":
        results = run_spike_load(args.api_url, 20, 80, 300, 900, args.duration)
    
    analysis = analyze_results(results)
    
    print("\n=== Test Results ===")
    print(f"Total Requests: {analysis['total_requests']}")
    print(f"Successful: {analysis['successful_requests']}")
    print(f"Failed: {analysis['failed_requests']}")
    print(f"Error Rate: {analysis['error_rate']:.2f}%")
    print(f"Throughput: {analysis['throughput_rps']:.2f} req/s")
    print(f"\nLatency:")
    print(f"  P50: {analysis['latency']['p50']:.2f}ms")
    print(f"  P95: {analysis['latency']['p95']:.2f}ms")
    print(f"  P99: {analysis['latency']['p99']:.2f}ms")
    
    # Save results
    output_data = {
        "analysis": analysis,
        "results": [asdict(r) for r in results]
    }
    
    with open(args.output, 'w') as f:
        json.dump(output_data, f, indent=2)
    
    print(f"\nResults saved to {args.output}")
