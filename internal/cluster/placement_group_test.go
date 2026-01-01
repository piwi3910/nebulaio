package cluster

import (
	"sync"
	"sync/atomic"
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
	mgr.Start(t.Context())
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	testLocalGroupOperations(t, mgr)
	testGetGroupOperations(t, mgr)
	testNodeGroupOperations(t, mgr)
	testLocalGroupNodeChecks(t, mgr)
	testReplicationAndAllGroupsChecks(t, mgr)
}

func testLocalGroupOperations(t *testing.T, mgr *PlacementGroupManager) {
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
}

func testGetGroupOperations(t *testing.T, mgr *PlacementGroupManager) {
	group, ok := mgr.GetGroup("pg-dc2")
	if !ok {
		t.Fatal("GetGroup failed for pg-dc2")
	}

	if group.Name != "Datacenter 2" {
		t.Errorf("Expected name 'Datacenter 2', got '%s'", group.Name)
	}
}

func testNodeGroupOperations(t *testing.T, mgr *PlacementGroupManager) {
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
}

func testLocalGroupNodeChecks(t *testing.T, mgr *PlacementGroupManager) {
	if !mgr.IsLocalGroupNode("node1") {
		t.Error("Expected node1 to be in local group")
	}

	if !mgr.IsLocalGroupNode("node2") {
		t.Error("Expected node2 to be in local group")
	}

	if mgr.IsLocalGroupNode("node4") {
		t.Error("Expected node4 to NOT be in local group")
	}

	nodes := mgr.LocalGroupNodes()
	if len(nodes) != 3 {
		t.Errorf("Expected 3 local nodes, got %d", len(nodes))
	}
}

func testReplicationAndAllGroupsChecks(t *testing.T, mgr *PlacementGroupManager) {
	targets := mgr.ReplicationTargets()
	if len(targets) != 1 {
		t.Errorf("Expected 1 replication target, got %d", len(targets))
	}

	if targets[0].ID != "pg-dc2" {
		t.Errorf("Expected replication target 'pg-dc2', got '%s'", targets[0].ID)
	}

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
	mgr.Start(t.Context())
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
	mgr.Start(t.Context())
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
	mgr.Start(t.Context())
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
	mgr.Start(t.Context())
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

func TestPlacementGroupManager_ConcurrentReads(t *testing.T) {
	config := PlacementGroupConfig{
		LocalGroupID: "pg-main",
		Groups: []PlacementGroup{
			{
				ID:         "pg-main",
				Name:       "Main",
				Datacenter: "dc1",
				Region:     "us-east-1",
				Nodes:      []string{"node1", "node2", "node3", "node4", "node5"},
				MinNodes:   2,
				MaxNodes:   10,
				Status:     PlacementGroupStatusHealthy,
			},
		},
		MinNodesForErasure: 3,
	}

	mgr, err := NewPlacementGroupManager(config)
	mgr.Start(t.Context())
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	const (
		numGoroutines = 100
		numIterations = 1000
	)

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for range numGoroutines {
		go func() {
			defer wg.Done()

			for range numIterations {
				// Concurrent reads should not panic or cause data races
				_ = mgr.LocalGroup()
				_ = mgr.AllGroups()
				_ = mgr.LocalGroupNodes()
				_, _ = mgr.GetGroup("pg-main")
				_, _ = mgr.GetNodeGroup("node1")
				_ = mgr.IsLocalGroupNode("node1")
				_ = mgr.ReplicationTargets()
				_ = mgr.CanPerformErasureCoding(4, 2)
				_, _ = mgr.GetShardPlacementNodes(3)
			}
		}()
	}

	wg.Wait()
}

func TestPlacementGroupManager_ConcurrentReadWrite(t *testing.T) {
	config := PlacementGroupConfig{
		LocalGroupID: "pg-main",
		Groups: []PlacementGroup{
			{
				ID:         "pg-main",
				Name:       "Main",
				Datacenter: "dc1",
				Region:     "us-east-1",
				Nodes:      []string{"node1", "node2"},
				MinNodes:   1,
				MaxNodes:   100, // High max to allow many concurrent adds
				Status:     PlacementGroupStatusHealthy,
			},
		},
		MinNodesForErasure: 3,
	}

	mgr, err := NewPlacementGroupManager(config)
	mgr.Start(t.Context())
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	const (
		numReaders    = 50
		numWriters    = 10
		numIterations = 100
	)

	var (
		wg          sync.WaitGroup
		nodeCounter int64
	)

	// Start readers

	wg.Add(numReaders)

	for range numReaders {
		go func() {
			defer wg.Done()

			for range numIterations {
				_ = mgr.LocalGroup()
				_ = mgr.AllGroups()
				_ = mgr.LocalGroupNodes()
				_, _ = mgr.GetShardPlacementNodes(2)
			}
		}()
	}

	// Start writers (add/remove nodes)
	wg.Add(numWriters)

	for i := range numWriters {
		go func(writerID int) {
			defer wg.Done()

			for range numIterations {
				nodeNum := atomic.AddInt64(&nodeCounter, 1)
				nodeID := string(rune('A'+writerID)) + string(rune('0'+nodeNum%10))

				// Add node (ignore errors as capacity may be reached)
				_ = mgr.AddNodeToGroup("pg-main", nodeID)

				// Remove it back
				_ = mgr.RemoveNodeFromGroup("pg-main", nodeID)
			}
		}(i)
	}

	wg.Wait()
}

func TestPlacementGroupManager_ConcurrentStatusUpdates(t *testing.T) {
	config := PlacementGroupConfig{
		LocalGroupID: "pg-main",
		Groups: []PlacementGroup{
			{
				ID:     "pg-main",
				Name:   "Main",
				Nodes:  []string{"node1", "node2", "node3"},
				Status: PlacementGroupStatusHealthy,
			},
		},
	}

	mgr, err := NewPlacementGroupManager(config)
	mgr.Start(t.Context())
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	const (
		numGoroutines = 50
		numIterations = 100
	)

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	statuses := []PlacementGroupStatus{
		PlacementGroupStatusHealthy,
		PlacementGroupStatusDegraded,
		PlacementGroupStatusOffline,
		PlacementGroupStatusUnknown,
	}

	for i := range numGoroutines {
		go func(id int) {
			defer wg.Done()

			for j := range numIterations {
				status := statuses[(id+j)%len(statuses)]
				_ = mgr.UpdateGroupStatus("pg-main", status)

				// Also do reads
				group := mgr.LocalGroup()
				_ = group.Status
			}
		}(i)
	}

	wg.Wait()
}

func TestPlacementGroupManager_ConcurrentCallbacks(t *testing.T) {
	config := PlacementGroupConfig{
		LocalGroupID: "pg-main",
		Groups: []PlacementGroup{
			{
				ID:       "pg-main",
				Name:     "Main",
				Nodes:    []string{},
				MinNodes: 0,
				MaxNodes: 1000,
			},
		},
	}

	mgr, err := NewPlacementGroupManager(config)
	mgr.Start(t.Context())
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	var joinCount, leaveCount, statusCount int64

	// Set callbacks
	mgr.SetOnNodeJoinedGroup(func(groupID PlacementGroupID, nodeID string) {
		atomic.AddInt64(&joinCount, 1)
	})
	mgr.SetOnNodeLeftGroup(func(groupID PlacementGroupID, nodeID string) {
		atomic.AddInt64(&leaveCount, 1)
	})
	mgr.SetOnGroupStatusChange(func(groupID PlacementGroupID, status PlacementGroupStatus) {
		atomic.AddInt64(&statusCount, 1)
	})

	const (
		numGoroutines = 20
		numIterations = 50
	)

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := range numGoroutines {
		go func(id int) {
			defer wg.Done()

			for j := range numIterations {
				nodeID := string(rune('A'+id)) + string(rune('0'+j%10))

				// Add and remove nodes (triggers callbacks)
				_ = mgr.AddNodeToGroup("pg-main", nodeID)
				_ = mgr.RemoveNodeFromGroup("pg-main", nodeID)
			}
		}(i)
	}

	wg.Wait()

	// Give callbacks time to complete (they run in goroutines)
	// Note: We can't check exact counts due to race conditions in add/remove,
	// but we verify no panics occurred
	t.Logf("Callbacks triggered - joins: %d, leaves: %d, status: %d",
		atomic.LoadInt64(&joinCount),
		atomic.LoadInt64(&leaveCount),
		atomic.LoadInt64(&statusCount))
}

func TestPlacementGroupManager_CopyPreventsExternalMutation(t *testing.T) {
	config := PlacementGroupConfig{
		LocalGroupID: "pg-main",
		Groups: []PlacementGroup{
			{
				ID:         "pg-main",
				Name:       "Main",
				Datacenter: "dc1",
				Region:     "us-east-1",
				Nodes:      []string{"node1", "node2", "node3"},
				MinNodes:   2,
				MaxNodes:   10,
				Status:     PlacementGroupStatusHealthy,
			},
		},
	}

	mgr, err := NewPlacementGroupManager(config)
	mgr.Start(t.Context())
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Get a copy and try to mutate it
	group := mgr.LocalGroup()
	originalNodes := len(group.Nodes)

	// Mutate the returned copy
	group.Nodes = append(group.Nodes, "malicious-node")
	group.Name = "Hacked!"
	group.Status = PlacementGroupStatusOffline

	// Verify original is unchanged
	actualGroup := mgr.LocalGroup()
	if len(actualGroup.Nodes) != originalNodes {
		t.Errorf("External mutation affected internal state: expected %d nodes, got %d",
			originalNodes, len(actualGroup.Nodes))
	}

	if actualGroup.Name != "Main" {
		t.Errorf("External mutation affected name: expected 'Main', got '%s'", actualGroup.Name)
	}

	if actualGroup.Status != PlacementGroupStatusHealthy {
		t.Errorf("External mutation affected status: expected 'healthy', got '%s'", actualGroup.Status)
	}

	// Same test for AllGroups
	allGroups := mgr.AllGroups()
	allGroups[0].Nodes = []string{}
	allGroups[0].Name = "Hacked!"

	actualGroup = mgr.LocalGroup()
	if len(actualGroup.Nodes) != originalNodes {
		t.Errorf("AllGroups mutation affected internal state")
	}
}

func TestPlacementGroupManager_GetShardPlacementNodesForObject(t *testing.T) {
	config := PlacementGroupConfig{
		LocalGroupID: "pg-main",
		Groups: []PlacementGroup{
			{
				ID:       "pg-main",
				Name:     "Main",
				Nodes:    []string{"node1", "node2", "node3", "node4", "node5", "node6"},
				MinNodes: 2,
				MaxNodes: 10,
			},
		},
	}

	mgr, err := NewPlacementGroupManager(config)
	mgr.Start(t.Context())
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Test deterministic placement - same object should always get same nodes
	nodes1, err := mgr.GetShardPlacementNodesForObject("bucket1", "key1", 3)
	if err != nil {
		t.Fatalf("Failed to get shard placement: %v", err)
	}

	nodes2, err := mgr.GetShardPlacementNodesForObject("bucket1", "key1", 3)
	if err != nil {
		t.Fatalf("Failed to get shard placement: %v", err)
	}

	// Should be identical
	for i := range nodes1 {
		if nodes1[i] != nodes2[i] {
			t.Errorf("Non-deterministic placement: position %d got '%s' then '%s'",
				i, nodes1[i], nodes2[i])
		}
	}

	// Different objects should (usually) get different starting positions
	nodesA, _ := mgr.GetShardPlacementNodesForObject("bucket1", "key1", 3)
	nodesB, _ := mgr.GetShardPlacementNodesForObject("bucket2", "different-key", 3)

	// At least one position should differ (with high probability)
	// We don't assert this as hash collisions are possible, just log
	allSame := true

	for i := range nodesA {
		if nodesA[i] != nodesB[i] {
			allSame = false
			break
		}
	}

	if allSame {
		t.Log("Note: Different objects got same placement (possible but unlikely)")
	}

	// Test distribution - run many objects and check distribution
	distribution := make(map[string]int)
	for i := range 1000 {
		key := string(rune('a'+i%26)) + string(rune('0'+i/26))

		nodes, _ := mgr.GetShardPlacementNodesForObject("test-bucket", key, 1)
		if len(nodes) > 0 {
			distribution[nodes[0]]++
		}
	}

	// Check all nodes got some objects
	for _, nodeID := range []string{"node1", "node2", "node3", "node4", "node5", "node6"} {
		count := distribution[nodeID]
		if count == 0 {
			t.Errorf("Node %s got no objects - distribution is not balanced", nodeID)
		}
		// Each node should get roughly 1000/6 = ~166 objects, allow wide tolerance
		if count < 50 || count > 400 {
			t.Errorf("Node %s got %d objects - may indicate poor distribution", nodeID, count)
		}
	}

	t.Logf("Distribution: %v", distribution)
}

// TestPlacementGroupManager_NilPointerHandling tests that nil pointers are handled gracefully.
func TestPlacementGroupManager_NilPointerHandling(t *testing.T) {
	// Test with empty config (no groups defined)
	emptyConfig := PlacementGroupConfig{
		LocalGroupID: "",
		Groups:       nil,
	}

	mgr, err := NewPlacementGroupManager(emptyConfig)
	if err != nil {
		t.Fatalf("Should handle empty config: %v", err)
	}

	testNilPointerLocalGroup(t, mgr)
	testNilPointerGetGroup(t, mgr)
	testNilPointerShardPlacement(t, mgr)
	testNilPointerReplicationAndGroups(t, mgr)
	testNilPointerGroupOperations(t, mgr)
	testNilPointerClose(t, mgr)
}

func testNilPointerLocalGroup(t *testing.T, mgr *PlacementGroupManager) {
	// LocalGroup should return nil gracefully
	if mgr.LocalGroup() != nil {
		t.Error("LocalGroup should return nil for empty config")
	}

	// LocalGroupNodes should return nil gracefully
	if mgr.LocalGroupNodes() != nil {
		t.Error("LocalGroupNodes should return nil for empty config")
	}
}

func testNilPointerGetGroup(t *testing.T, mgr *PlacementGroupManager) {
	// GetGroup for non-existent group
	group, ok := mgr.GetGroup("nonexistent")
	if ok || group != nil {
		t.Error("GetGroup should return nil for non-existent group")
	}
}

func testNilPointerShardPlacement(t *testing.T, mgr *PlacementGroupManager) {
	// GetShardPlacementNodes with no local group should return empty, no panic
	nodes, err := mgr.GetShardPlacementNodes(3)
	if err != nil {
		t.Error("GetShardPlacementNodes should not error with no local group")
	}

	if len(nodes) != 0 {
		t.Error("GetShardPlacementNodes should return empty slice with no local group")
	}

	// GetShardPlacementNodesForObject with no local group
	nodes, err = mgr.GetShardPlacementNodesForObject("bucket", "key", 3)
	if err == nil {
		t.Error("GetShardPlacementNodesForObject should error with no local group")
	}

	if len(nodes) != 0 {
		t.Error("GetShardPlacementNodesForObject should return empty slice on error")
	}
}

func testNilPointerReplicationAndGroups(t *testing.T, mgr *PlacementGroupManager) {
	// ReplicationTargets should return empty, not panic
	targets := mgr.ReplicationTargets()
	if len(targets) != 0 {
		t.Error("ReplicationTargets should return empty slice for empty config")
	}

	// AllGroups should return empty slice, not panic
	allGroups := mgr.AllGroups()
	if allGroups == nil {
		t.Error("AllGroups should return empty slice, not nil")
	}

	if len(allGroups) != 0 {
		t.Error("AllGroups should be empty for empty config")
	}
}

func testNilPointerGroupOperations(t *testing.T, mgr *PlacementGroupManager) {
	// AddNodeToGroup on non-existent group should error gracefully
	err := mgr.AddNodeToGroup("nonexistent", "node1")
	if err == nil {
		t.Error("AddNodeToGroup should error for non-existent group")
	}

	// RemoveNodeFromGroup on non-existent group should error gracefully
	err = mgr.RemoveNodeFromGroup("nonexistent", "node1")
	if err == nil {
		t.Error("RemoveNodeFromGroup should error for non-existent group")
	}

	// UpdateGroupStatus on non-existent group should error gracefully
	err = mgr.UpdateGroupStatus("nonexistent", PlacementGroupStatusHealthy)
	if err == nil {
		t.Error("UpdateGroupStatus should error for non-existent group")
	}
}

func testNilPointerClose(t *testing.T, mgr *PlacementGroupManager) {
	// Close should not panic on empty manager
	err := mgr.Close()
	if err != nil {
		t.Errorf("Close should not error: %v", err)
	}
}

// TestPlacementGroupManager_UnhealthyGroupOperations tests operations when group becomes unhealthy.
func TestPlacementGroupManager_UnhealthyGroupOperations(t *testing.T) {
	config := PlacementGroupConfig{
		LocalGroupID: "pg-main",
		Groups: []PlacementGroup{
			{
				ID:         "pg-main",
				Name:       "Main Group",
				Datacenter: "dc1",
				Region:     "us-east-1",
				Nodes:      []string{"node1", "node2", "node3", "node4"},
				MinNodes:   3,
				MaxNodes:   10,
			},
		},
	}

	mgr, err := NewPlacementGroupManager(config)
	mgr.Start(t.Context())
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	defer mgr.Close()

	testUnhealthyGroupHealthyState(t, mgr)
	testUnhealthyGroupDegradeToMinNodes(t, mgr)
	testUnhealthyGroupRemoveAllNodes(t, mgr)
	testUnhealthyGroupRecovery(t, mgr)
}

func testUnhealthyGroupHealthyState(t *testing.T, mgr *PlacementGroupManager) {
	// Verify initial healthy status
	group := mgr.LocalGroup()
	if group.Status != PlacementGroupStatusHealthy {
		t.Errorf("Expected healthy status, got %s", group.Status)
	}

	// Shard placement should work when healthy
	nodes, err := mgr.GetShardPlacementNodesForObject("bucket", "key", 3)
	if err != nil {
		t.Errorf("Shard placement should work when healthy: %v", err)
	}

	if len(nodes) != 3 {
		t.Errorf("Expected 3 nodes, got %d", len(nodes))
	}
}

func testUnhealthyGroupDegradeToMinNodes(t *testing.T, mgr *PlacementGroupManager) {
	// Remove nodes to make the group degraded (below MinNodes)
	err := mgr.RemoveNodeFromGroup("pg-main", "node4")
	if err != nil {
		t.Fatalf("Failed to remove node: %v", err)
	}

	err = mgr.RemoveNodeFromGroup("pg-main", "node3")
	if err != nil {
		t.Fatalf("Failed to remove node: %v", err)
	}

	// Verify group is now degraded
	group := mgr.LocalGroup()
	if group.Status != PlacementGroupStatusDegraded {
		t.Errorf("Expected degraded status, got %s", group.Status)
	}

	// Shard placement should still work (with 2 nodes for 2 shards)
	nodes, err := mgr.GetShardPlacementNodesForObject("bucket", "key", 2)
	if err != nil {
		t.Errorf("Shard placement should work when degraded: %v", err)
	}

	if len(nodes) != 2 {
		t.Errorf("Expected 2 nodes, got %d", len(nodes))
	}

	// Shard placement should fail when requesting more shards than nodes
	_, err = mgr.GetShardPlacementNodesForObject("bucket", "key", 3)
	if err == nil {
		t.Error("Shard placement should fail when requesting more shards than available nodes")
	}
}

func testUnhealthyGroupRemoveAllNodes(t *testing.T, mgr *PlacementGroupManager) {
	// Remove all remaining nodes
	err := mgr.RemoveNodeFromGroup("pg-main", "node2")
	if err != nil {
		t.Fatalf("Failed to remove node: %v", err)
	}

	err = mgr.RemoveNodeFromGroup("pg-main", "node1")
	if err != nil {
		t.Fatalf("Failed to remove node: %v", err)
	}

	// Verify group has no nodes
	group := mgr.LocalGroup()
	if len(group.Nodes) != 0 {
		t.Errorf("Expected 0 nodes, got %d", len(group.Nodes))
	}

	// Shard placement should fail with no nodes
	_, err = mgr.GetShardPlacementNodesForObject("bucket", "key", 1)
	if err == nil {
		t.Error("Shard placement should fail with no nodes")
	}
}

func testUnhealthyGroupRecovery(t *testing.T, mgr *PlacementGroupManager) {
	// Add nodes back and verify recovery
	err := mgr.AddNodeToGroup("pg-main", "node5")
	if err != nil {
		t.Fatalf("Failed to add node: %v", err)
	}

	err = mgr.AddNodeToGroup("pg-main", "node6")
	if err != nil {
		t.Fatalf("Failed to add node: %v", err)
	}

	err = mgr.AddNodeToGroup("pg-main", "node7")
	if err != nil {
		t.Fatalf("Failed to add node: %v", err)
	}

	// Verify recovery to healthy
	group := mgr.LocalGroup()
	if group.Status != PlacementGroupStatusHealthy {
		t.Errorf("Expected healthy status after recovery, got %s", group.Status)
	}

	// Shard placement should work again
	nodes, err := mgr.GetShardPlacementNodesForObject("bucket", "key", 3)
	if err != nil {
		t.Errorf("Shard placement should work after recovery: %v", err)
	}

	if len(nodes) != 3 {
		t.Errorf("Expected 3 nodes after recovery, got %d", len(nodes))
	}
}

// TestPlacementGroupManager_NumShardsValidation tests validation of numShards parameter.
func TestPlacementGroupManager_NumShardsValidation(t *testing.T) {
	config := PlacementGroupConfig{
		LocalGroupID: "pg-main",
		Groups: []PlacementGroup{
			{
				ID:         "pg-main",
				Name:       "Main Group",
				Datacenter: "dc1",
				Region:     "us-east-1",
				Nodes:      []string{"node1", "node2", "node3"},
				MinNodes:   2,
			},
		},
	}

	mgr, err := NewPlacementGroupManager(config)
	mgr.Start(t.Context())
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	defer mgr.Close()

	testCases := []struct {
		name      string
		numShards int
		wantErr   bool
	}{
		{"zero shards", 0, true},
		{"negative shards", -1, true},
		{"one shard", 1, false},
		{"two shards", 2, false},
		{"three shards", 3, false},
		{"more than nodes", 4, true},
		{"way more than nodes", 100, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			nodes, err := mgr.GetShardPlacementNodesForObject("bucket", "key", tc.numShards)
			if tc.wantErr {
				if err == nil {
					t.Errorf("Expected error for numShards=%d, got none", tc.numShards)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for numShards=%d: %v", tc.numShards, err)
				}

				if len(nodes) != tc.numShards {
					t.Errorf("Expected %d nodes, got %d", tc.numShards, len(nodes))
				}
			}
		})
	}
}
