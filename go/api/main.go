/*
 * Copyright (c) 2025 Ishaan Nene
 *
 * This source code is licensed under the MIT license found in the
 * LICENSE file in the root directory of this source tree.
 */
/*
This file consists of Important API definitions and functions to handle incoming requests.
There are some important strucuts as well like MoveRequest, MoveResponse, Job, JobResult.
*/
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
	"github.com/go-redis/redis/v8"
)

var (
	rdb *redis.Client
	ctx = context.Background()
)

type MoveRequest struct {
	FEN        string `json:"fen"`
	Elo        int    `json:"elo"`         
	MoveTimeMs int    `json:"movetime_ms"` 
}

type MoveResponse struct {
	BestMove string `json:"bestmove"`
	Ponder   string `json:"ponder,omitempty"`
	Info     string `json:"info,omitempty"`
}

type Job struct {
	JobID   string `json:"job_id"`
	FEN     string `json:"fen"`
	Elo     int    `json:"elo"`
	MaxTime int    `json:"max_time_ms"`
}

type JobResult struct {
	JobID    string `json:"job_id"`
	BestMove string `json:"bestmove"`
	Ponder   string `json:"ponder,omitempty"`
	Info     string `json:"info,omitempty"`
	Error    string `json:"error,omitempty"`
}

func main() {
	redisAddr := getenv("REDIS_ADDR", "redis:6379")
	rdb = redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		log.Printf("Warning: Redis connection failed: %v", err)
	} else {
		log.Printf("Connected to Redis at %s", redisAddr)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/move", func(w http.ResponseWriter, r *http.Request) {
		enableCORS(&w, r)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}

		var req MoveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.FEN == "" {
			http.Error(w, "missing fen", http.StatusBadRequest)
			return
		}
		
		if req.Elo == 0 {
			req.Elo = 1600
		}
		if req.Elo < 1320 {
			req.Elo = 1320
		}
		if req.Elo > 3190 {
			req.Elo = 3190
		}
		if req.MoveTimeMs <= 0 {
			req.MoveTimeMs = 1000
		}
		jobID := fmt.Sprintf("job_%d_%d", time.Now().UnixNano(), req.Elo)
		job := Job{
			JobID:   jobID,
			FEN:     req.FEN,
			Elo:     req.Elo,
			MaxTime: req.MoveTimeMs,
		}

		log.Printf("Processing move request: jobID=%s, elo=%d, moveTime=%dms, fen=%s", 
			jobID, req.Elo, req.MoveTimeMs, req.FEN[:20]+"...")

		jobData, err := json.Marshal(job)
		if err != nil {
			log.Printf("Error marshaling job %s: %v", jobID, err)
			http.Error(w, "failed to serialize job", http.StatusInternalServerError)
			return
		}

		if err := rdb.LPush(ctx, "stockfish:jobs", jobData).Err(); err != nil {
			log.Printf("Error queuing job %s: %v", jobID, err)
			http.Error(w, "failed to queue job: "+err.Error(), http.StatusServiceUnavailable)
			return
		}
		timeout := time.Duration(req.MoveTimeMs+5000) * time.Millisecond
		result, err := waitForResult(jobID, timeout)
		if err != nil {
			log.Printf("Job %s failed or timed out: %v", jobID, err)
			http.Error(w, "job timeout or error: "+err.Error(), http.StatusRequestTimeout)
			return
		}

		log.Printf("Job %s completed successfully: bestmove=%s", jobID, result.BestMove)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(MoveResponse{
			BestMove: result.BestMove,
			Ponder:   result.Ponder,
			Info:     result.Info,
		})
	})

	addr := ":8080"
	log.Printf("API listening on %s (redis=%s)", addr, redisAddr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func waitForResult(jobID string, timeout time.Duration) (*JobResult, error) {
	deadline := time.Now().Add(timeout)
	
	for time.Now().Before(deadline) {
		results, err := rdb.LRange(ctx, "stockfish:results", 0, -1).Result()
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		for _, resultStr := range results {
			var result JobResult
			if err := json.Unmarshal([]byte(resultStr), &result); err != nil {
				continue
			}

			if result.JobID == jobID {
				rdb.LRem(ctx, "stockfish:results", 1, resultStr)
				
				if result.Error != "" {
					return nil, fmt.Errorf("engine error: %s", result.Error)
				}
				return &result, nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	return nil, fmt.Errorf("timeout waiting for job result")
}

func enableCORS(w *http.ResponseWriter, r *http.Request) {
	(*w).Header().Set("Access-Control-Allow-Origin", getenv("CORS_ALLOW_ORIGIN", "*"))
	(*w).Header().Set("Access-Control-Allow-Headers", "Content-Type")
	(*w).Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	(*w).Header().Set("Content-Type", "application/json")
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
