package config

import (
	"testing"
)

func TestPlacementGroupsConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  PlacementGroupsConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty config is valid",
			config:  PlacementGroupsConfig{},
			wantErr: false,
		},
		{
			name: "valid config with single group",
			config: PlacementGroupsConfig{
				LocalGroupID: "pg1",
				Groups: []PlacementGroupConfig{
					{
						ID:       "pg1",
						MinNodes: 3,
						MaxNodes: 10,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid config with multiple groups",
			config: PlacementGroupsConfig{
				LocalGroupID: "pg1",
				Groups: []PlacementGroupConfig{
					{ID: "pg1", MinNodes: 3, MaxNodes: 10},
					{ID: "pg2", MinNodes: 2, MaxNodes: 5},
				},
				ReplicationTargets: []string{"pg2"},
			},
			wantErr: false,
		},
		{
			name: "empty group ID is invalid",
			config: PlacementGroupsConfig{
				Groups: []PlacementGroupConfig{
					{ID: "", MinNodes: 3, MaxNodes: 10},
				},
			},
			wantErr: true,
			errMsg:  "placement group ID cannot be empty",
		},
		{
			name: "duplicate group ID is invalid",
			config: PlacementGroupsConfig{
				Groups: []PlacementGroupConfig{
					{ID: "pg1", MinNodes: 3, MaxNodes: 10},
					{ID: "pg1", MinNodes: 2, MaxNodes: 5},
				},
			},
			wantErr: true,
			errMsg:  "duplicate placement group ID: pg1",
		},
		{
			name: "negative min_nodes is invalid",
			config: PlacementGroupsConfig{
				Groups: []PlacementGroupConfig{
					{ID: "pg1", MinNodes: -1, MaxNodes: 10},
				},
			},
			wantErr: true,
			errMsg:  "min_nodes cannot be negative",
		},
		{
			name: "negative max_nodes is invalid",
			config: PlacementGroupsConfig{
				Groups: []PlacementGroupConfig{
					{ID: "pg1", MinNodes: 3, MaxNodes: -1},
				},
			},
			wantErr: true,
			errMsg:  "max_nodes cannot be negative",
		},
		{
			name: "min_nodes exceeding max_nodes is invalid",
			config: PlacementGroupsConfig{
				Groups: []PlacementGroupConfig{
					{ID: "pg1", MinNodes: 10, MaxNodes: 5},
				},
			},
			wantErr: true,
			errMsg:  "min_nodes (10) cannot exceed max_nodes (5)",
		},
		{
			name: "unknown local_group_id is invalid",
			config: PlacementGroupsConfig{
				LocalGroupID: "unknown",
				Groups: []PlacementGroupConfig{
					{ID: "pg1", MinNodes: 3, MaxNodes: 10},
				},
			},
			wantErr: true,
			errMsg:  "local_group_id \"unknown\" references unknown placement group",
		},
		{
			name: "unknown replication_target is invalid",
			config: PlacementGroupsConfig{
				LocalGroupID:       "pg1",
				ReplicationTargets: []string{"unknown"},
				Groups: []PlacementGroupConfig{
					{ID: "pg1", MinNodes: 3, MaxNodes: 10},
				},
			},
			wantErr: true,
			errMsg:  "replication_target \"unknown\" references unknown placement group",
		},
		{
			name: "negative min_nodes_for_erasure is invalid",
			config: PlacementGroupsConfig{
				MinNodesForErasure: -1,
			},
			wantErr: true,
			errMsg:  "min_nodes_for_erasure cannot be negative",
		},
		{
			name: "zero max_nodes allows any min_nodes",
			config: PlacementGroupsConfig{
				Groups: []PlacementGroupConfig{
					{ID: "pg1", MinNodes: 100, MaxNodes: 0},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("validate() expected error containing %q, got nil", tt.errMsg)
					return
				}
				if tt.errMsg != "" && err.Error() != tt.errMsg && !contains(err.Error(), tt.errMsg) {
					t.Errorf("validate() error = %q, want containing %q", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("validate() unexpected error: %v", err)
				}
			}
		})
	}
}

// contains checks if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestConfig_ValidateRedundancyVsNodes(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "disabled redundancy skips validation",
			config: Config{
				Storage: StorageConfig{
					DefaultRedundancy: RedundancyConfig{
						Enabled:      false,
						DataShards:   100, // Would fail if enabled
						ParityShards: 100,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid redundancy with sufficient min_nodes_for_erasure",
			config: Config{
				Storage: StorageConfig{
					DefaultRedundancy: RedundancyConfig{
						Enabled:      true,
						DataShards:   10,
						ParityShards: 4,
					},
					PlacementGroups: PlacementGroupsConfig{
						MinNodesForErasure: 14,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "min_nodes_for_erasure less than total shards",
			config: Config{
				Storage: StorageConfig{
					DefaultRedundancy: RedundancyConfig{
						Enabled:      true,
						DataShards:   10,
						ParityShards: 4,
					},
					PlacementGroups: PlacementGroupsConfig{
						MinNodesForErasure: 10, // Less than 14
					},
				},
			},
			wantErr: true,
			errMsg:  "min_nodes_for_erasure (10) is less than total shards required",
		},
		{
			name: "placement group min_nodes less than total shards",
			config: Config{
				Storage: StorageConfig{
					DefaultRedundancy: RedundancyConfig{
						Enabled:      true,
						DataShards:   10,
						ParityShards: 4,
					},
					PlacementGroups: PlacementGroupsConfig{
						Groups: []PlacementGroupConfig{
							{ID: "pg1", MinNodes: 10, MaxNodes: 20}, // MinNodes < 14
						},
					},
				},
			},
			wantErr: true,
			errMsg:  "min_nodes (10) is less than total shards required (14)",
		},
		{
			name: "placement group max_nodes less than total shards",
			config: Config{
				Storage: StorageConfig{
					DefaultRedundancy: RedundancyConfig{
						Enabled:      true,
						DataShards:   10,
						ParityShards: 4,
					},
					PlacementGroups: PlacementGroupsConfig{
						Groups: []PlacementGroupConfig{
							{ID: "pg1", MinNodes: 0, MaxNodes: 10}, // MaxNodes < 14
						},
					},
				},
			},
			wantErr: true,
			errMsg:  "max_nodes (10) is less than total shards required (14)",
		},
		{
			name: "valid placement group with sufficient nodes",
			config: Config{
				Storage: StorageConfig{
					DefaultRedundancy: RedundancyConfig{
						Enabled:      true,
						DataShards:   10,
						ParityShards: 4,
					},
					PlacementGroups: PlacementGroupsConfig{
						Groups: []PlacementGroupConfig{
							{ID: "pg1", MinNodes: 14, MaxNodes: 20},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "replication factor exceeds available targets",
			config: Config{
				Storage: StorageConfig{
					DefaultRedundancy: RedundancyConfig{
						Enabled:           true,
						DataShards:        10,
						ParityShards:      4,
						ReplicationFactor: 3,
					},
					PlacementGroups: PlacementGroupsConfig{
						ReplicationTargets: []string{"pg2"}, // Only 1 target
					},
				},
			},
			wantErr: true,
			errMsg:  "replication_factor (3) exceeds number of available replication targets (1)",
		},
		{
			name: "valid replication factor with sufficient targets",
			config: Config{
				Storage: StorageConfig{
					DefaultRedundancy: RedundancyConfig{
						Enabled:           true,
						DataShards:        10,
						ParityShards:      4,
						ReplicationFactor: 2,
					},
					PlacementGroups: PlacementGroupsConfig{
						ReplicationTargets: []string{"pg2", "pg3"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "zero min_nodes_for_erasure skips that check",
			config: Config{
				Storage: StorageConfig{
					DefaultRedundancy: RedundancyConfig{
						Enabled:      true,
						DataShards:   10,
						ParityShards: 4,
					},
					PlacementGroups: PlacementGroupsConfig{
						MinNodesForErasure: 0, // Not set
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.validateRedundancyVsNodes()
			if tt.wantErr {
				if err == nil {
					t.Errorf("validateRedundancyVsNodes() expected error containing %q, got nil", tt.errMsg)
					return
				}
				if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("validateRedundancyVsNodes() error = %q, want containing %q", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("validateRedundancyVsNodes() unexpected error: %v", err)
				}
			}
		})
	}
}
