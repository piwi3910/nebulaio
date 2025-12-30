package events

import (
	"context"
	"errors"
)

// Target is the interface for event notification targets.
type Target interface {
	// Name returns the target name
	Name() string

	// Type returns the target type (webhook, kafka, etc.)
	Type() string

	// Publish sends an event to the target
	Publish(ctx context.Context, event *S3Event) error

	// IsHealthy checks if the target is healthy
	IsHealthy(ctx context.Context) bool

	// Close closes the target connection
	Close() error
}

// TargetConfig is the base configuration for targets.
type TargetConfig struct {
	// Name is the unique target name
	Name string `json:"name" yaml:"name"`

	// Type is the target type
	Type string `json:"type" yaml:"type"`

	// Enabled indicates if this target is active
	Enabled bool `json:"enabled" yaml:"enabled"`

	// QueueSize is the size of the event queue for async publishing
	QueueSize int `json:"queueSize,omitempty" yaml:"queueSize,omitempty"`

	// MaxRetries is the maximum number of retries for failed publishes
	MaxRetries int `json:"maxRetries,omitempty" yaml:"maxRetries,omitempty"`
}

// Common target errors.
var (
	ErrTargetClosed    = errors.New("target is closed")
	ErrTargetUnhealthy = errors.New("target is unhealthy")
	ErrPublishFailed   = errors.New("failed to publish event")
	ErrTargetNotFound  = errors.New("target not found")
	ErrInvalidConfig   = errors.New("invalid target configuration")
)

// TargetFactory creates targets from configuration.
type TargetFactory func(config map[string]interface{}) (Target, error)

// targetFactories holds registered target factories.
var targetFactories = make(map[string]TargetFactory)

// RegisterTargetFactory registers a target factory.
func RegisterTargetFactory(targetType string, factory TargetFactory) {
	targetFactories[targetType] = factory
}

// CreateTarget creates a target from configuration.
func CreateTarget(targetType string, config map[string]interface{}) (Target, error) {
	factory, ok := targetFactories[targetType]
	if !ok {
		return nil, errors.New("unknown target type: " + targetType)
	}

	return factory(config)
}

// GetRegisteredTargetTypes returns the list of registered target types.
func GetRegisteredTargetTypes() []string {
	types := make([]string, 0, len(targetFactories))
	for t := range targetFactories {
		types = append(types, t)
	}

	return types
}
