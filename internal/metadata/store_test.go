package metadata

import (
	"strings"
	"testing"
)

func TestRedundancyConfig_Validate(t *testing.T) {
	tests := []struct {
		config  *RedundancyConfig
		name    string
		errMsg  string
		wantErr bool
	}{
		{
			name:    "nil config is valid",
			config:  nil,
			wantErr: false,
		},
		{
			name:    "disabled config is valid regardless of values",
			config:  &RedundancyConfig{Enabled: false, DataShards: 0, ParityShards: 0},
			wantErr: false,
		},
		{
			name: "valid enabled config",
			config: &RedundancyConfig{
				Enabled:      true,
				DataShards:   10,
				ParityShards: 4,
			},
			wantErr: false,
		},
		{
			name: "valid config with all fields",
			config: &RedundancyConfig{
				Enabled:            true,
				DataShards:         10,
				ParityShards:       4,
				MinAvailableShards: 10,
				PlacementPolicy:    "spread",
				ReplicationFactor:  2,
			},
			wantErr: false,
		},
		{
			name: "data_shards below minimum",
			config: &RedundancyConfig{
				Enabled:      true,
				DataShards:   1,
				ParityShards: 1,
			},
			wantErr: true,
			errMsg:  "data_shards must be at least 2",
		},
		{
			name: "data_shards above maximum",
			config: &RedundancyConfig{
				Enabled:      true,
				DataShards:   257,
				ParityShards: 1,
			},
			wantErr: true,
			errMsg:  "data_shards must be at most 256",
		},
		{
			name: "parity_shards below minimum",
			config: &RedundancyConfig{
				Enabled:      true,
				DataShards:   10,
				ParityShards: 0,
			},
			wantErr: true,
			errMsg:  "parity_shards must be at least 1",
		},
		{
			name: "parity_shards above maximum when added with data",
			config: &RedundancyConfig{
				Enabled:      true,
				DataShards:   200,
				ParityShards: 100,
			},
			wantErr: true,
			errMsg:  "total shards (data + parity) must be at most 256",
		},
		{
			name: "min_available_shards less than data_shards",
			config: &RedundancyConfig{
				Enabled:            true,
				DataShards:         10,
				ParityShards:       4,
				MinAvailableShards: 5,
			},
			wantErr: true,
			errMsg:  "min_available_shards (5) cannot be less than data_shards (10)",
		},
		{
			name: "invalid placement_policy",
			config: &RedundancyConfig{
				Enabled:         true,
				DataShards:      10,
				ParityShards:    4,
				PlacementPolicy: "invalid-policy",
			},
			wantErr: true,
			errMsg:  "invalid placement_policy",
		},
		{
			name: "valid placement_policy spread",
			config: &RedundancyConfig{
				Enabled:         true,
				DataShards:      10,
				ParityShards:    4,
				PlacementPolicy: "spread",
			},
			wantErr: false,
		},
		{
			name: "valid placement_policy local",
			config: &RedundancyConfig{
				Enabled:         true,
				DataShards:      10,
				ParityShards:    4,
				PlacementPolicy: "local",
			},
			wantErr: false,
		},
		{
			name: "valid placement_policy rack-aware",
			config: &RedundancyConfig{
				Enabled:         true,
				DataShards:      10,
				ParityShards:    4,
				PlacementPolicy: "rack-aware",
			},
			wantErr: false,
		},
		{
			name: "valid placement_policy zone-aware",
			config: &RedundancyConfig{
				Enabled:         true,
				DataShards:      10,
				ParityShards:    4,
				PlacementPolicy: "zone-aware",
			},
			wantErr: false,
		},
		{
			name: "empty placement_policy is valid",
			config: &RedundancyConfig{
				Enabled:         true,
				DataShards:      10,
				ParityShards:    4,
				PlacementPolicy: "",
			},
			wantErr: false,
		},
		{
			name: "negative replication_factor",
			config: &RedundancyConfig{
				Enabled:           true,
				DataShards:        10,
				ParityShards:      4,
				ReplicationFactor: -1,
			},
			wantErr: true,
			errMsg:  "replication_factor cannot be negative",
		},
		{
			name: "replication_factor exceeds maximum",
			config: &RedundancyConfig{
				Enabled:           true,
				DataShards:        10,
				ParityShards:      4,
				ReplicationFactor: 11,
			},
			wantErr: true,
			errMsg:  "replication_factor cannot exceed 10",
		},
		{
			name: "maximum valid replication_factor",
			config: &RedundancyConfig{
				Enabled:           true,
				DataShards:        10,
				ParityShards:      4,
				ReplicationFactor: 10,
			},
			wantErr: false,
		},
		{
			name: "boundary: exactly 256 total shards",
			config: &RedundancyConfig{
				Enabled:      true,
				DataShards:   200,
				ParityShards: 56,
			},
			wantErr: false,
		},
		{
			name: "boundary: minimum valid data_shards",
			config: &RedundancyConfig{
				Enabled:      true,
				DataShards:   2,
				ParityShards: 1,
			},
			wantErr: false,
		},
		{
			name: "boundary: maximum valid data_shards with min parity",
			config: &RedundancyConfig{
				Enabled:      true,
				DataShards:   255,
				ParityShards: 1,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error containing %q, got nil", tt.errMsg)
					return
				}

				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %q, want containing %q", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error: %v", err)
				}
			}
		})
	}
}
