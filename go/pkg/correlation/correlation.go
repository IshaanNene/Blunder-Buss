package correlation

import (
	"context"
	"fmt"
	"math/rand"
	"time"
)

// IDGenerator generates correlation IDs
type IDGenerator struct {
	serviceName string
	rng         *rand.Rand
}

// NewIDGenerator creates a new correlation ID generator
func NewIDGenerator(serviceName string) *IDGenerator {
	return &IDGenerator{
		serviceName: serviceName,
		rng:         rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// Generate creates a new correlation ID
// Format: {service}-{timestamp}-{random}
// Example: api-1699564823-a3f9c2
// Requirement 8.1: Generate unique correlation ID
func (g *IDGenerator) Generate() string {
	timestamp := time.Now().Unix()
	random := g.rng.Intn(0xFFFFFF) // 24-bit random number
	return fmt.Sprintf("%s-%d-%06x", g.serviceName, timestamp, random)
}

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

const (
	// CorrelationIDKey is the context key for correlation ID
	CorrelationIDKey contextKey = "correlation_id"
)

// WithID adds correlation ID to context
// Requirement 8.2, 8.3: Store correlation ID in context for propagation
func WithID(ctx context.Context, correlationID string) context.Context {
	return context.WithValue(ctx, CorrelationIDKey, correlationID)
}

// FromContext retrieves correlation ID from context
func FromContext(ctx context.Context) (string, bool) {
	correlationID, ok := ctx.Value(CorrelationIDKey).(string)
	return correlationID, ok
}

// GetOrGenerate retrieves correlation ID from context or generates a new one
func GetOrGenerate(ctx context.Context, generator *IDGenerator) (string, context.Context) {
	if correlationID, ok := FromContext(ctx); ok && correlationID != "" {
		return correlationID, ctx
	}
	
	correlationID := generator.Generate()
	return correlationID, WithID(ctx, correlationID)
}

// ExtractFromHeader extracts correlation ID from HTTP header value
// Requirement 8.1: Extract correlation ID from X-Correlation-ID header
func ExtractFromHeader(headerValue string) string {
	return headerValue
}

// Validate checks if a correlation ID is valid (non-empty)
func Validate(correlationID string) bool {
	return correlationID != ""
}
