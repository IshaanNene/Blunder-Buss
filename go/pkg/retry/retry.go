package retry

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"time"
)

// Config holds retry configuration
type Config struct {
	MaxAttempts   int           // Maximum number of retry attempts
	InitialDelay  time.Duration // Initial delay before first retry
	MaxDelay      time.Duration // Maximum delay between retries
	Multiplier    float64       // Backoff multiplier
	JitterPercent float64       // Jitter percentage (0.0 to 1.0)
	OnRetry       func(attempt int, delay time.Duration, err error) // Optional callback for retry attempts
}

// WithRetry executes the given function with retry logic and exponential backoff
// Requirements 4.1, 4.2, 4.5, 4.7: exponential backoff with jitter
func WithRetry(ctx context.Context, cfg Config, fn func() error) error {
	var lastErr error
	
	for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
		// Execute the function
		err := fn()
		if err == nil {
			return nil
		}
		
		lastErr = err
		
		// Don't sleep after the last attempt
		if attempt == cfg.MaxAttempts-1 {
			break
		}
		
		// Calculate backoff delay with exponential backoff
		delay := calculateBackoff(cfg, attempt)
		
		// Call retry callback if provided (Requirement 4.7: Log each retry attempt)
		if cfg.OnRetry != nil {
			cfg.OnRetry(attempt+1, delay, err)
		}
		
		// Check if context is cancelled
		select {
		case <-ctx.Done():
			return fmt.Errorf("retry cancelled: %w", ctx.Err())
		case <-time.After(delay):
			// Continue to next attempt
		}
	}
	
	return fmt.Errorf("all retry attempts exhausted: %w", lastErr)
}

// calculateBackoff calculates the backoff delay with exponential backoff and jitter
// Formula: delay = min(initialDelay * (multiplier ^ attempt), maxDelay)
// Then apply jitter: delay * (1 + random(-jitterPercent, +jitterPercent))
func calculateBackoff(cfg Config, attempt int) time.Duration {
	// Calculate exponential backoff
	delay := float64(cfg.InitialDelay) * math.Pow(cfg.Multiplier, float64(attempt))
	
	// Cap at max delay
	if delay > float64(cfg.MaxDelay) {
		delay = float64(cfg.MaxDelay)
	}
	
	// Apply jitter if configured
	if cfg.JitterPercent > 0 {
		jitter := delay * cfg.JitterPercent * (2*rand.Float64() - 1) // Random between -jitterPercent and +jitterPercent
		delay += jitter
		
		// Ensure delay is not negative
		if delay < 0 {
			delay = float64(cfg.InitialDelay)
		}
	}
	
	return time.Duration(delay)
}

// StockfishRetryConfig returns the configuration for Worker → Stockfish connections
// Requirements 4.1, 4.2, 4.5, 4.7: 3 max attempts, exponential backoff starting at 100ms, 20% jitter
func StockfishRetryConfig() Config {
	return Config{
		MaxAttempts:   3,                       // Maximum 3 retries
		InitialDelay:  100 * time.Millisecond,  // Start at 100ms
		MaxDelay:      5 * time.Second,         // Cap at 5 seconds
		Multiplier:    2.0,                     // Exponential backoff
		JitterPercent: 0.2,                     // 20% jitter to prevent thundering herd
	}
}

// RedisPublishRetryConfig returns the configuration for API → Redis job publishing
// Requirement 4.3: 2 max attempts, 50ms fixed delay
func RedisPublishRetryConfig() Config {
	return Config{
		MaxAttempts:   2,                      // Up to 2 retries
		InitialDelay:  50 * time.Millisecond,  // Fixed 50ms delay
		MaxDelay:      50 * time.Millisecond,  // Fixed delay (no exponential)
		Multiplier:    1.0,                    // No exponential backoff
		JitterPercent: 0.0,                    // No jitter
	}
}

// RedisResultRetryConfig returns the configuration for Worker → Redis result publishing
// Requirement 4.4: 3 max attempts, exponential backoff with jitter
func RedisResultRetryConfig() Config {
	return Config{
		MaxAttempts:   3,                       // Up to 3 retries
		InitialDelay:  100 * time.Millisecond,  // Start at 100ms
		MaxDelay:      5 * time.Second,         // Cap at 5 seconds
		Multiplier:    2.0,                     // Exponential backoff
		JitterPercent: 0.2,                     // 20% jitter
	}
}

// GetBackoffDuration returns the backoff duration for a given attempt (useful for logging)
func GetBackoffDuration(cfg Config, attempt int) time.Duration {
	return calculateBackoff(cfg, attempt)
}
