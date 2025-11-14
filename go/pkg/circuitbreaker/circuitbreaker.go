package circuitbreaker

import (
	"fmt"
	"time"

	"github.com/sony/gobreaker"
)

// State represents the circuit breaker state
type State int

const (
	StateClosed State = iota
	StateHalfOpen
	StateOpen
)

// String returns the string representation of the state
func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateHalfOpen:
		return "half-open"
	case StateOpen:
		return "open"
	default:
		return "unknown"
	}
}

// Config holds circuit breaker configuration
type Config struct {
	FailureThreshold uint32        // Number of failures before opening
	SuccessThreshold uint32        // Number of successes to close from half-open
	Timeout          time.Duration // Time to wait before transitioning to half-open
	MaxRequests      uint32        // Max requests allowed in half-open state
}

// CircuitBreaker wraps sony/gobreaker with custom metrics integration
type CircuitBreaker struct {
	breaker       *gobreaker.CircuitBreaker
	config        Config
	onStateChange func(from, to State)
}

// Metrics holds circuit breaker metrics
type Metrics struct {
	State         State
	Failures      uint32
	Successes     uint32
	Requests      uint32
	ConsecutiveFails uint32
}

// New creates a new circuit breaker with the given configuration
func New(name string, config Config) *CircuitBreaker {
	cb := &CircuitBreaker{
		config: config,
	}
	
	settings := gobreaker.Settings{
		Name:        name,
		MaxRequests: config.MaxRequests,
		Interval:    0, // No automatic reset
		Timeout:     config.Timeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			// Open circuit after FailureThreshold consecutive failures
			return counts.ConsecutiveFailures >= config.FailureThreshold
		},
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			if cb.onStateChange != nil {
				cb.onStateChange(convertState(from), convertState(to))
			}
		},
	}
	
	cb.breaker = gobreaker.NewCircuitBreaker(settings)
	
	return cb
}

// Call executes the given function with circuit breaker protection
func (cb *CircuitBreaker) Call(fn func() error) error {
	_, err := cb.breaker.Execute(func() (interface{}, error) {
		return nil, fn()
	})
	return err
}

// State returns the current circuit breaker state
func (cb *CircuitBreaker) State() State {
	return convertState(cb.breaker.State())
}

// Metrics returns current circuit breaker metrics
func (cb *CircuitBreaker) Metrics() Metrics {
	counts := cb.breaker.Counts()
	return Metrics{
		State:            cb.State(),
		Failures:         uint32(counts.TotalFailures),
		Successes:        uint32(counts.TotalSuccesses),
		Requests:         uint32(counts.Requests),
		ConsecutiveFails: uint32(counts.ConsecutiveFailures),
	}
}

// OnStateChange registers a callback for state changes
func (cb *CircuitBreaker) OnStateChange(fn func(from, to State)) {
	cb.onStateChange = fn
}

// convertState converts gobreaker.State to our State type
func convertState(s gobreaker.State) State {
	switch s {
	case gobreaker.StateClosed:
		return StateClosed
	case gobreaker.StateHalfOpen:
		return StateHalfOpen
	case gobreaker.StateOpen:
		return StateOpen
	default:
		return StateClosed
	}
}

// IsOpen returns true if the circuit breaker is open
func (cb *CircuitBreaker) IsOpen() bool {
	return cb.State() == StateOpen
}

// IsHalfOpen returns true if the circuit breaker is half-open
func (cb *CircuitBreaker) IsHalfOpen() bool {
	return cb.State() == StateHalfOpen
}

// IsClosed returns true if the circuit breaker is closed
func (cb *CircuitBreaker) IsClosed() bool {
	return cb.State() == StateClosed
}

// ErrCircuitOpen is returned when the circuit breaker is open
var ErrCircuitOpen = fmt.Errorf("circuit breaker is open")

// StockfishCircuitBreakerConfig returns the configuration for Worker → Stockfish circuit breaker
// Requirements 3.1-3.5: 5 failures in 60s threshold, 30s timeout
func StockfishCircuitBreakerConfig() Config {
	return Config{
		FailureThreshold: 5,              // Open after 5 failures
		Timeout:          30 * time.Second, // Wait 30s before half-open
		SuccessThreshold: 1,              // Close after 1 success in half-open
		MaxRequests:      1,              // Allow 1 test request in half-open
	}
}

// RedisCircuitBreakerConfig returns the configuration for API → Redis circuit breaker
// Requirements 3.6-3.7: 3 failures in 30s threshold, 30s timeout
func RedisCircuitBreakerConfig() Config {
	return Config{
		FailureThreshold: 3,              // Open after 3 failures
		Timeout:          30 * time.Second, // Wait 30s before half-open
		SuccessThreshold: 1,              // Close after 1 success in half-open
		MaxRequests:      1,              // Allow 1 test request in half-open
	}
}

// MetricsCollector interface for circuit breaker metrics
type MetricsCollector interface {
	SetCircuitBreakerState(service, component string, state float64)
	IncrementCircuitBreakerFailures(service, component string)
}

// NewStockfishCircuitBreaker creates a circuit breaker for Stockfish connections with metrics
// Requirement 3.8: Expose circuit breaker state metrics
// Requirements 3.1-3.5: 5 failures in 60s threshold, 30s timeout
func NewStockfishCircuitBreaker(metricsCol MetricsCollector) *gobreaker.CircuitBreaker {
	config := StockfishCircuitBreakerConfig()
	
	settings := gobreaker.Settings{
		Name:        "stockfish",
		MaxRequests: config.MaxRequests,
		Interval:    60 * time.Second, // 60s window for failure counting (Requirement 3.1)
		Timeout:     config.Timeout,   // 30s timeout before half-open (Requirement 3.1)
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			// Open after 5 failures within the 60s interval (Requirement 3.1)
			failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
			return counts.ConsecutiveFailures >= config.FailureThreshold || 
			       (counts.Requests >= config.FailureThreshold && failureRatio >= 0.5)
		},
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			// Update metrics on state change (Requirement 3.8)
			var stateValue float64
			switch to {
			case gobreaker.StateClosed:
				stateValue = 0
			case gobreaker.StateHalfOpen:
				stateValue = 1
			case gobreaker.StateOpen:
				stateValue = 2
			}
			
			if metricsCol != nil {
				metricsCol.SetCircuitBreakerState("stockfish", "worker", stateValue)
			}
			
			// Increment failure count when opening (Requirement 3.8)
			if to == gobreaker.StateOpen && metricsCol != nil {
				metricsCol.IncrementCircuitBreakerFailures("stockfish", "worker")
			}
		},
	}
	
	return gobreaker.NewCircuitBreaker(settings)
}

// NewRedisCircuitBreaker creates a circuit breaker for Redis connections with metrics
func NewRedisCircuitBreaker(metricsCol MetricsCollector) *gobreaker.CircuitBreaker {
	config := RedisCircuitBreakerConfig()
	
	settings := gobreaker.Settings{
		Name:        "redis",
		MaxRequests: config.MaxRequests,
		Interval:    0,
		Timeout:     config.Timeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= config.FailureThreshold
		},
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			var stateValue float64
			switch to {
			case gobreaker.StateClosed:
				stateValue = 0
			case gobreaker.StateHalfOpen:
				stateValue = 1
			case gobreaker.StateOpen:
				stateValue = 2
			}
			
			if metricsCol != nil {
				metricsCol.SetCircuitBreakerState("redis", "worker", stateValue)
			}
			
			if to == gobreaker.StateOpen && metricsCol != nil {
				metricsCol.IncrementCircuitBreakerFailures("redis", "worker")
			}
		},
	}
	
	return gobreaker.NewCircuitBreaker(settings)
}
