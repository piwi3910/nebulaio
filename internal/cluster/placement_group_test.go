package cluster

import (
	"testing"
)

func TestPlacementGroupManager_BasicOperations(t *testing.T) {
	// Create a configuration with two placement groups
	config := PlacementGroupConfig{
		LocalGroupID: "pg-dc1",
		Groups: []PlacementGroup{
			{
				ID:         "pg-dc1",
				Name:       "Datacenter 1",
				Datacenter: "dc1",
				Region:     "us-east-1",
				Nodes:      []string{"node1", "node2", "node3"},
				MinNodes:   2,
				MaxNodes:   5,
			},
			{
				ID:         "pg-dc2",
				Name:       "Datacenter 2",
				Datacenter: "dc2",
				Region:     "us-west-1",
				Nodes:      []string{"node4", "node5", "node6"},
				MinNodes:   2,
				MaxNodes:   5,
			},
		},
		MinNodesForErasure: 3,
		ReplicationTargets: []PlacementGroupID{"pg-dc2"},
	}

	mgr, err := NewPlacementGroupManager(config)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Test LocalGroup
	localGroup := mgr.LocalGroup()
	if localGroup == nil {
		t.Fatal("LocalGroup returned nil")
	}
	if localGroup.ID != "pg-dc1" {
		t.Errorf("Expected local group ID 'pg-dc1', got '%s'", localGroup.ID)
	}
	if !localGroup.IsLocal {
		t.Error("Expected local group IsLocal to be true")
	}

	// Test GetGroup
	group, ok := mgr.GetGroup("pg-dc2")
	if !ok {
		t.Fatal("GetGroup failed for pg-dc2")
	}
	if group.Name != "Datacenter 2" {
		t.Errorf("Expected name 'Datacenter 2', got '%s'", group.Name)
	}

	// Test GetNodeGroup
	nodeGroup, ok := mgr.GetNodeGroup("node1")
	if !ok {
		t.Fatal("GetNodeGroup failed for node1")
	}
	if nodeGroup.ID != "pg-dc1" {
		t.Errorf("Expected node1 in group 'pg-dc1', got '%s'", nodeGroup.ID)
	}

	nodeGroup, ok = mgr.GetNodeGroup("node5")
	if !ok {
		t.Fatal("GetNodeGroup failed for node5")
	}
	if nodeGroup.ID != "pg-dc2" {
		t.Errorf("Expected node5 in group 'pg-dc2', got '%s'", nodeGroup.ID)
	}

	// Test IsLocalGroupNode
	if !mgr.IsLocalGroupNode("node1") {
		t.Error("Expected node1 to be in local group")
	}
	if !mgr.IsLocalGroupNode("node2") {
		t.Error("Expected node2 to be in local group")
	}
	if mgr.IsLocalGroupNode("node4") {
		t.Error("Expected node4 to NOT be in local group")
	}

	// Test LocalGroupNodes
	nodes := mgr.LocalGroupNodes()
	if len(nodes) != 3 {
		t.Errorf("Expected 3 local nodes, got %d", len(nodes))
	}

	// Test ReplicationTargets
	targets := mgr.ReplicationTargets()
	if len(targets) != 1 {
		t.Errorf("Expected 1 replication target, got %d", len(targets))
	}
	if targets[0].ID != "pg-dc2" {
		t.Errorf("Expected replication target 'pg-dc2', got '%s'", targets[0].ID)
	}

	// Test AllGroups
	allGroups := mgr.AllGroups()
	if len(allGroups) != 2 {
		t.Errorf("Expected 2 groups, got %d", len(allGroups))
	}
}

func TestPlacementGroupManager_NodeOperations(t *testing.T) {
	config := PlacementGroupConfig{
		LocalGroupID: "pg-main",
		Groups: []PlacementGroup{
			{
				ID:       "pg-main",
				Name:     "Main",
				Nodes:    []string{"node1"},
				MinNodes: 2,
				MaxNodes: 3,
			},
		},
	}

	mgr, err := NewPlacementGroupManager(config)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Add a node
	err = mgr.AddNodeToGroup("pg-main", "node2")
	if err != nil {
		t.Fatalf("Failed to add node: %v", err)
	}

	nodes := mgr.LocalGroupNodes()
	if len(nodes) != 2 {
		t.Errorf("Expected 2 nodes after add, got %d", len(nodes))
	}

	// Add another node
	err = mgr.AddNodeToGroup("pg-main", "node3")
	if err != nil {
		t.Fatalf("Failed to add node: %v", err)
	}

	// Try to add beyond max
	err = mgr.AddNodeToGroup("pg-main", "node4")
	if err == nil {
		t.Error("Expected error when adding beyond max nodes")
	}

	// Remove a node
	err = mgr.RemoveNodeFromGroup("pg-main", "node2")
	if err != nil {
		t.Fatalf("Failed to remove node: %v", err)
	}

	nodes = mgr.LocalGroupNodes()
	if len(nodes) != 2 {
		t.Errorf("Expected 2 nodes after remove, got %d", len(nodes))
	}

	// Verify node2 is gone
	_, ok := mgr.GetNodeGroup("node2")
	if ok {
		t.Error("Expected node2 to not be in any group after removal")
	}
}

func TestPlacementGroupManager_ErasureCoding(t *testing.T) {
	config := PlacementGroupConfig{
		LocalGroupID: "pg-main",
		Groups: []PlacementGroup{
			{
				ID:       "pg-main",
				Name:     "Main",
				Nodes:    []string{"node1", "node2", "node3", "node4"},
				MinNodes: 2,
				MaxNodes: 10,
			},
		},
		MinNodesForErasure: 3,
	}

	mgr, err := NewPlacementGroupManager(config)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Test CanPerformErasureCoding
	// 4 nodes available, so 4+2 should work but 8+4 shouldn't
	if !mgr.CanPerformErasureCoding(4, 0) {
		t.Error("Expected to be able to do 4+0 erasure coding")
	}

	if !mgr.CanPerformErasureCoding(2, 2) {
		t.Error("Expected to be able to do 2+2 erasure coding")
	}

	if mgr.CanPerformErasureCoding(8, 4) {
		t.Error("Expected NOT to be able to do 8+4 erasure coding with only 4 nodes")
	}

	// Test GetShardPlacementNodes
	nodes, err := mgr.GetShardPlacementNodes(3)
	if err != nil {
		t.Fatalf("Failed to get shard placement nodes: %v", err)
	}
	if len(nodes) != 3 {
		t.Errorf("Expected 3 placement nodes, got %d", len(nodes))
	}

	// Request more nodes than available
	_, err = mgr.GetShardPlacementNodes(10)
	if err == nil {
		t.Error("Expected error when requesting more nodes than available")
	}
}

func TestPlacementGroupManager_StatusUpdate(t *testing.T) {
	config := PlacementGroupConfig{
		LocalGroupID: "pg-main",
		Groups: []PlacementGroup{
			{
				ID:     "pg-main",
				Name:   "Main",
				Nodes:  []string{"node1"},
				Status: PlacementGroupStatusHealthy,
			},
		},
	}

	mgr, err := NewPlacementGroupManager(config)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Update status
	err = mgr.UpdateGroupStatus("pg-main", PlacementGroupStatusDegraded)
	if err != nil {
		t.Fatalf("Failed to update status: %v", err)
	}

	group := mgr.LocalGroup()
	if group.Status != PlacementGroupStatusDegraded {
		t.Errorf("Expected status 'degraded', got '%s'", group.Status)
	}

	// Try to update non-existent group
	err = mgr.UpdateGroupStatus("pg-invalid", PlacementGroupStatusHealthy)
	if err == nil {
		t.Error("Expected error when updating non-existent group")
	}
}

func TestPlacementGroupManager_SingleNodeMode(t *testing.T) {
	// Empty configuration - single node mode
	config := PlacementGroupConfig{
		LocalGroupID: "",
		Groups:       []PlacementGroup{},
	}

	mgr, err := NewPlacementGroupManager(config)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// In single node mode, local group is nil
	if mgr.LocalGroup() != nil {
		t.Error("Expected nil local group in single node mode")
	}

	// But erasure coding should still be possible (locally)
	if !mgr.CanPerformErasureCoding(4, 2) {
		t.Error("Expected erasure coding to be possible in single node mode")
	}

	// GetShardPlacementNodes returns empty in single node mode
	nodes, err := mgr.GetShardPlacementNodes(3)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("Expected 0 nodes in single node mode, got %d", len(nodes))
	}
}

func TestPlacementGroupNodeMeta(t *testing.T) {
	meta := PlacementGroupNodeMeta{
		PlacementGroupID: "pg-dc1",
		Datacenter:       "dc1",
		Region:           "us-east-1",
	}

	// Encode
	data, err := EncodeNodeMeta(meta)
	if err != nil {
		t.Fatalf("Failed to encode node meta: %v", err)
	}

	// Decode
	decoded, err := DecodeNodeMeta(data)
	if err != nil {
		t.Fatalf("Failed to decode node meta: %v", err)
	}

	if decoded.PlacementGroupID != meta.PlacementGroupID {
		t.Errorf("Expected PlacementGroupID '%s', got '%s'", meta.PlacementGroupID, decoded.PlacementGroupID)
	}
	if decoded.Datacenter != meta.Datacenter {
		t.Errorf("Expected Datacenter '%s', got '%s'", meta.Datacenter, decoded.Datacenter)
	}
	if decoded.Region != meta.Region {
		t.Errorf("Expected Region '%s', got '%s'", meta.Region, decoded.Region)
	}
}
