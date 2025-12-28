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
