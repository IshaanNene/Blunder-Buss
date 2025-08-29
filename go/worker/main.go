/*
 * Copyright (c) 2025 Ishaan Nene
 *
 * This source code is licensed under the MIT license found in the
 * LICENSE file in the root directory of this source tree.
 */
/*
This file consists of the worker code that processes jobs from the Redis queue, interacts with the chess engine, and publishes results back to Redis.
Please note: the struct definitions for Job and JobResult are repeated here for clarity, even though they are also defined in api/main.go.
Dont change the definitions
*/
package main

import (
    "bufio"
    "context"
    "encoding/json"
    "fmt"
    "log"
    "net"
    "os"
    "strings"
    "time"
    "github.com/go-redis/redis/v8"
)

var (
    rdb *redis.Client
    ctx = context.Background()
)

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
    rdb = redis.NewClient(&redis.Options{Addr: redisAddr})
    engineAddr := getenv("ENGINE_ADDR", "stockfish:4000")
    _, err := rdb.Ping(ctx).Result()
    if err != nil {
        log.Printf("Warning: Redis connection failed: %v", err)
    } else {
        log.Printf("Connected to Redis at %s", redisAddr)
    }
    log.Printf("Worker started (redis=%s engine=%s)", redisAddr, engineAddr)
    for {
        res, err := rdb.BLPop(ctx, 5*time.Second, "stockfish:jobs").Result()
        if err != nil {
            if err == redis.Nil {
                continue
            }
            log.Println("redis error:", err)
            time.Sleep(1 * time.Second)
            continue
        }
        if len(res) < 2 {
            continue
        }
        
        var job Job
        if err := json.Unmarshal([]byte(res[1]), &job); err != nil {
            log.Println("invalid job payload", err)
            continue
        }
        
        log.Printf("Got job %s fen=%s elo=%d time=%dms", job.JobID, job.FEN, job.Elo, job.MaxTime)
        go handleJob(engineAddr, job)
    }
}

func handleJob(engineAddr string, job Job) {
    result := JobResult{
        JobID: job.JobID,
    }
    conn, err := net.DialTimeout("tcp", engineAddr, 5*time.Second)
    if err != nil {
        result.Error = fmt.Sprintf("engine connect error: %v", err)
        publishResult(result)
        return
    }
    defer conn.Close()

    reader := bufio.NewReader(conn)

    write := func(cmd string) error {
        _, err := fmt.Fprintf(conn, "%s\n", cmd)
        return err
    }

    readUntil := func(substr string, timeout time.Duration) (string, error) {
        deadline := time.Now().Add(timeout)
        var lines []string
        
        for time.Now().Before(deadline) {
            conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
            line, err := reader.ReadString('\n')
            if err != nil {
                if ne, ok := err.(net.Error); ok && ne.Timeout() {
                    continue
                }
                return strings.Join(lines, "\n"), err
            }
            line = strings.TrimSpace(line)
            lines = append(lines, line)
            if strings.Contains(line, substr) {
                return strings.Join(lines, "\n"), nil
            }
        }
        return strings.Join(lines, "\n"), fmt.Errorf("timeout waiting for %q", substr)
    }
    if err := write("uci"); err != nil {
        result.Error = fmt.Sprintf("uci command error: %v", err)
        publishResult(result)
        return
    }

    if _, err := readUntil("uciok", 3*time.Second); err != nil {
        result.Error = fmt.Sprintf("uci init error: %v", err)
        publishResult(result)
        return
    }

    if job.Elo > 0 {
        if err := write("setoption name UCI_LimitStrength value true"); err != nil {
            log.Printf("Warning: failed to set limit strength: %v", err)
        }
        if err := write(fmt.Sprintf("setoption name UCI_Elo value %d", job.Elo)); err != nil {
            log.Printf("Warning: failed to set ELO: %v", err)
        }
    }
    if err := write("isready"); err != nil {
        result.Error = fmt.Sprintf("isready command error: %v", err)
        publishResult(result)
        return
    }

    if _, err := readUntil("readyok", 2*time.Second); err != nil {
        result.Error = fmt.Sprintf("ready check error: %v", err)
        publishResult(result)
        return
    }
    if err := write("ucinewgame"); err != nil {
        result.Error = fmt.Sprintf("ucinewgame command error: %v", err)
        publishResult(result)
        return
    }
    if strings.TrimSpace(job.FEN) == "" {
        write("position startpos")
    } else {
        write(fmt.Sprintf("position fen %s", job.FEN))
    }

    moveTimeMs := job.MaxTime
    if moveTimeMs <= 0 {
        moveTimeMs = 1000
    }
    
    if err := write(fmt.Sprintf("go movetime %d", moveTimeMs)); err != nil {
        result.Error = fmt.Sprintf("go command error: %v", err)
        publishResult(result)
        return
    }

    deadline := time.Now().Add(time.Duration(moveTimeMs+5000) * time.Millisecond)
    var infoLines []string
    
    for time.Now().Before(deadline) {
        conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
        line, err := reader.ReadString('\n')
        if err != nil {
            if ne, ok := err.(net.Error); ok && ne.Timeout() {
                continue
            }
            result.Error = fmt.Sprintf("engine read error: %v", err)
            publishResult(result)
            return
        }
        
        line = strings.TrimSpace(line)
        if line == "" {
            continue
        }

        if strings.HasPrefix(line, "info ") {
            infoLines = append(infoLines, line)
        } else if strings.HasPrefix(line, "bestmove ") {
            fields := strings.Fields(line)
            if len(fields) >= 2 {
                result.BestMove = fields[1]
            }
            if len(fields) >= 4 && fields[2] == "ponder" {
                result.Ponder = fields[3]
            }
            result.Info = strings.Join(infoLines, "\n")
            
            if result.BestMove == "" {
                result.Error = "no bestmove received"
            }
            
            publishResult(result)
            return
        }
    }

    result.Error = "timeout waiting for bestmove"
    publishResult(result)
}

func publishResult(result JobResult) {
    data, err := json.Marshal(result)
    if err != nil {
        log.Printf("Error marshaling result for job %s: %v", result.JobID, err)
        return
    }

    if err := rdb.RPush(ctx, "stockfish:results", data).Err(); err != nil {
        log.Printf("Error publishing result for job %s: %v", result.JobID, err)
        return
    }

    if result.Error != "" {
        log.Printf("Job %s completed with error: %s", result.JobID, result.Error)
    } else {
        log.Printf("Job %s completed successfully: %s", result.JobID, result.BestMove)
    }
}

func getenv(k, def string) string {
    v := os.Getenv(k)
    if v == "" {
        return def
    }
    return v
}