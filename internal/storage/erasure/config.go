package erasure

import (
	"errors"
	"fmt"

	"github.com/piwi3910/nebulaio/internal/cluster"
)

// Configuration constants for erasure coding.
const (
	defaultDataShards   = 10
	defaultParityShards = 4
	defaultShardSize    = 1024 * 1024 // 1MB
	minDataShards       = 2
	maxTotalShards      = 256
	minShardSize        = 1024
	percentMultiplierEC = 100
)

// Config holds erasure coding configuration.
type Config struct {
	PlacementGroupManager *cluster.PlacementGroupManager `json:"-" mapstructure:"-"`
	DataDir               string                         `json:"data_dir" mapstructure:"data_dir"`
	DataShards            int                            `json:"data_shards" mapstructure:"data_shards"`
	ParityShards          int                            `json:"parity_shards" mapstructure:"parity_shards"`
	ShardSize             int                            `json:"shard_size" mapstructure:"shard_size"`
}

// DefaultConfig returns the default erasure coding configuration.
func DefaultConfig() Config {
	return Config{
		DataShards:   defaultDataShards,
		ParityShards: defaultParityShards,
		ShardSize:    defaultShardSize,
		DataDir:      "/data/shards",
	}
}

// Preset represents a pre-configured erasure coding setup.
type Preset string

const (
	// PresetMinimal uses 4+2 configuration (can lose 2 shards).
	PresetMinimal Preset = "minimal"

	// PresetStandard uses 10+4 configuration (can lose 4 shards).
	PresetStandard Preset = "standard"

	// PresetMaximum uses 8+8 configuration (can lose 8 shards, 50% overhead).
	PresetMaximum Preset = "maximum"
)

// ConfigFromPreset returns a configuration based on a preset name.
func ConfigFromPreset(preset Preset, dataDir string) Config {
	cfg := DefaultConfig()
	cfg.DataDir = dataDir

	switch preset {
	case PresetMinimal:
		cfg.DataShards = 4
		cfg.ParityShards = 2
	case PresetStandard:
		cfg.DataShards = 10
		cfg.ParityShards = 4
	case PresetMaximum:
		cfg.DataShards = 8
		cfg.ParityShards = 8
	default:
		// Use standard as default
		cfg.DataShards = 10
		cfg.ParityShards = 4
	}

	return cfg
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.DataShards < minDataShards {
		return fmt.Errorf("data_shards must be at least %d, got %d", minDataShards, c.DataShards)
	}

	if c.DataShards > maxTotalShards {
		return fmt.Errorf("data_shards must be at most %d, got %d", maxTotalShards, c.DataShards)
	}

	if c.ParityShards < 1 {
		return fmt.Errorf("parity_shards must be at least 1, got %d", c.ParityShards)
	}

	if c.ParityShards > maxTotalShards {
		return fmt.Errorf("parity_shards must be at most %d, got %d", maxTotalShards, c.ParityShards)
	}

	if c.DataShards+c.ParityShards > maxTotalShards {
		return fmt.Errorf("total shards (data + parity) must be at most %d, got %d", maxTotalShards, c.DataShards+c.ParityShards)
	}

	if c.ShardSize < minShardSize {
		return fmt.Errorf("shard_size must be at least %d bytes, got %d", minShardSize, c.ShardSize)
	}

	if c.DataDir == "" {
		return errors.New("data_dir is required")
	}

	return nil
}

// TotalShards returns the total number of shards (data + parity).
func (c *Config) TotalShards() int {
	return c.DataShards + c.ParityShards
}

// RequiredShards returns the minimum number of shards needed to reconstruct data.
func (c *Config) RequiredShards() int {
	return c.DataShards
}

// MaxLoss returns the maximum number of shards that can be lost.
func (c *Config) MaxLoss() int {
	return c.ParityShards
}

// StorageOverhead returns the storage overhead as a percentage.
func (c *Config) StorageOverhead() float64 {
	return float64(c.ParityShards) / float64(c.DataShards) * percentMultiplierEC
}
