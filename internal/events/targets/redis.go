package targets

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/piwi3910/nebulaio/internal/events"
)

// RedisConfig configures a Redis target.
type RedisConfig struct {
	events.TargetConfig `yaml:",inline"`

	// Address is the Redis server address
	Address string `json:"address" yaml:"address"`

	// Password for authentication
	Password string `json:"-" yaml:"password,omitempty"`

	// Database number
	DB int `json:"db,omitempty" yaml:"db,omitempty"`

	// Channel is the Redis pub/sub channel
	Channel string `json:"channel" yaml:"channel"`

	// UsePubSub indicates whether to use pub/sub or streams
	UsePubSub bool `json:"usePubSub,omitempty" yaml:"usePubSub,omitempty"`

	// Stream is the Redis stream name (if not using pub/sub)
	Stream string `json:"stream,omitempty" yaml:"stream,omitempty"`

	// MaxLen is the maximum stream length (for XADD with MAXLEN)
	MaxLen int64 `json:"maxLen,omitempty" yaml:"maxLen,omitempty"`

	// TLS configuration
	TLSEnabled    bool   `json:"tlsEnabled,omitempty"    yaml:"tlsEnabled,omitempty"`
	TLSCACert     string `json:"tlsCaCert,omitempty"     yaml:"tlsCaCert,omitempty"`
	TLSClientCert string `json:"tlsClientCert,omitempty" yaml:"tlsClientCert,omitempty"`
	TLSClientKey  string `json:"-"                       yaml:"tlsClientKey,omitempty"`
	TLSSkipVerify bool   `json:"tlsSkipVerify,omitempty" yaml:"tlsSkipVerify,omitempty"`

	// Pool settings
	PoolSize     int           `json:"poolSize,omitempty"     yaml:"poolSize,omitempty"`
	MinIdleConns int           `json:"minIdleConns,omitempty" yaml:"minIdleConns,omitempty"`
	DialTimeout  time.Duration `json:"dialTimeout,omitempty"  yaml:"dialTimeout,omitempty"`
	ReadTimeout  time.Duration `json:"readTimeout,omitempty"  yaml:"readTimeout,omitempty"`
	WriteTimeout time.Duration `json:"writeTimeout,omitempty" yaml:"writeTimeout,omitempty"`
}

// DefaultRedisConfig returns a default Redis configuration.
func DefaultRedisConfig() RedisConfig {
	// Use REDIS_URL environment variable if set, otherwise use default
	redisAddr := os.Getenv("REDIS_URL")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}

	return RedisConfig{
		TargetConfig: events.TargetConfig{
			Type:       "redis",
			Enabled:    true,
			QueueSize:  10000,
			MaxRetries: 3,
		},
		Address:      redisAddr,
		DB:           0,
		Channel:      "s3:events",
		UsePubSub:    true,
		PoolSize:     10,
		MinIdleConns: 2,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	}
}

// RedisTarget publishes events to Redis
// Note: This is a placeholder implementation. In production, you would use
// github.com/redis/go-redis/v9.
type RedisTarget struct {
	config    RedisConfig
	mu        sync.RWMutex
	closed    bool
	connected bool
	// In production: client *redis.Client
}

// NewRedisTarget creates a new Redis target.
func NewRedisTarget(config RedisConfig) (*RedisTarget, error) {
	if config.Address == "" {
		return nil, fmt.Errorf("%w: address is required", events.ErrInvalidConfig)
	}

	if config.UsePubSub && config.Channel == "" {
		return nil, fmt.Errorf("%w: channel is required for pub/sub mode", events.ErrInvalidConfig)
	}

	if !config.UsePubSub && config.Stream == "" {
		return nil, fmt.Errorf("%w: stream is required for streams mode", events.ErrInvalidConfig)
	}

	t := &RedisTarget{
		config: config,
	}

	// In production, you would establish the Redis connection here:
	// client := redis.NewClient(&redis.Options{
	//     Addr:         config.Address,
	//     Password:     config.Password,
	//     DB:           config.DB,
	//     PoolSize:     config.PoolSize,
	//     MinIdleConns: config.MinIdleConns,
	// })
	// ...

	t.connected = true

	return t, nil
}

// Name returns the target name.
func (t *RedisTarget) Name() string {
	return t.config.Name
}

// Type returns the target type.
func (t *RedisTarget) Type() string {
	return "redis"
}

// Publish sends an event to Redis.
func (t *RedisTarget) Publish(ctx context.Context, event *events.S3Event) error {
	t.mu.RLock()

	if t.closed {
		t.mu.RUnlock()
		return events.ErrTargetClosed
	}

	t.mu.RUnlock()

	body, err := event.ToJSON()
	if err != nil {
		return fmt.Errorf("failed to serialize event: %w", err)
	}

	// In production, you would publish to Redis:
	// if t.config.UsePubSub {
	//     err = t.client.Publish(ctx, t.config.Channel, body).Err()
	// } else {
	//     args := &redis.XAddArgs{
	//         Stream: t.config.Stream,
	//         Values: map[string]interface{}{"event": body},
	//     }
	//     if t.config.MaxLen > 0 {
	//         args.MaxLen = t.config.MaxLen
	//     }
	//     _, err = t.client.XAdd(ctx, args).Result()
	// }
	_ = body // Placeholder

	return nil
}

// IsHealthy checks if the Redis connection is healthy.
func (t *RedisTarget) IsHealthy(ctx context.Context) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return !t.closed && t.connected
}

// Close closes the Redis target.
func (t *RedisTarget) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}

	t.closed = true
	t.connected = false
	// In production: return t.client.Close()
	return nil
}

// Ensure RedisTarget implements Target.
var _ events.Target = (*RedisTarget)(nil)

func init() {
	events.RegisterTargetFactory("redis", func(config map[string]interface{}) (events.Target, error) {
		cfg := DefaultRedisConfig()

		if name, ok := config["name"].(string); ok {
			cfg.Name = name
		}

		if address, ok := config["address"].(string); ok {
			cfg.Address = address
		}

		if password, ok := config["password"].(string); ok {
			cfg.Password = password
		}

		if db, ok := config["db"].(int); ok {
			cfg.DB = db
		}

		if channel, ok := config["channel"].(string); ok {
			cfg.Channel = channel
		}

		if usePubSub, ok := config["usePubSub"].(bool); ok {
			cfg.UsePubSub = usePubSub
		}

		if stream, ok := config["stream"].(string); ok {
			cfg.Stream = stream
		}

		return NewRedisTarget(cfg)
	})
}
