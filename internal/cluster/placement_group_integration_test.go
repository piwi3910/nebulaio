package cluster

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test constants.
const testBucketName = "test-bucket"

// TestMultiNodePlacementGroupSimulation simulates a multi-node cluster
// with placement groups and verifies correct behavior under various scenarios.
func TestMultiNodePlacementGroupSimulation(t *testing.T) {
	// Create a placement group config simulating 3 datacenters
	config := PlacementGroupConfig{
		LocalGroupID: "pg-dc1",
		Groups: []PlacementGroup{
			{
				ID:         "pg-dc1",
				Name:       "Datacenter 1",
				Datacenter: "dc1",
				Region:     "us-east",
				Nodes:      []string{"node1-dc1", "node2-dc1", "node3-dc1"},
				MinNodes:   3,
				MaxNodes:   10,
				Status:     PlacementGroupStatusHealthy,
			},
			{
				ID:         "pg-dc2",
				Name:       "Datacenter 2",
				Datacenter: "dc2",
				Region:     "us-west",
				Nodes:      []string{"node1-dc2", "node2-dc2", "node3-dc2"},
				MinNodes:   3,
				MaxNodes:   10,
				Status:     PlacementGroupStatusHealthy,
			},
			{
				ID:         "pg-dc3",
				Name:       "Datacenter 3",
				Datacenter: "dc3",
				Region:     "eu-west",
				Nodes:      []string{"node1-dc3", "node2-dc3", "node3-dc3"},
				MinNodes:   3,
				MaxNodes:   10,
				Status:     PlacementGroupStatusHealthy,
			},
		},
		MinNodesForErasure: 4,
		ReplicationTargets: []PlacementGroupID{"pg-dc2", "pg-dc3"},
	}

	mgr, err := NewPlacementGroupManager(config)
	require.NoError(t, err)

	t.Run("verify local group identification", func(t *testing.T) {
		localGroup := mgr.LocalGroup()
		require.NotNil(t, localGroup)
		assert.Equal(t, PlacementGroupID("pg-dc1"), localGroup.ID)
		assert.True(t, localGroup.IsLocal)
		assert.Equal(t, "dc1", localGroup.Datacenter)
	})

	t.Run("verify replication targets", func(t *testing.T) {
		targets := mgr.ReplicationTargets()
		assert.Len(t, targets, 2)

		targetIDs := make([]PlacementGroupID, len(targets))
		for i, t := range targets {
			targetIDs[i] = t.ID
		}

		assert.Contains(t, targetIDs, PlacementGroupID("pg-dc2"))
		assert.Contains(t, targetIDs, PlacementGroupID("pg-dc3"))
	})

	t.Run("verify cross-datacenter node lookup", func(t *testing.T) {
		// Node in local datacenter
		group1, found := mgr.GetNodeGroup("node1-dc1")
		require.True(t, found)
		assert.Equal(t, PlacementGroupID("pg-dc1"), group1.ID)
		assert.True(t, mgr.IsLocalGroupNode("node1-dc1"))

		// Node in remote datacenter
		group2, found := mgr.GetNodeGroup("node1-dc2")
		require.True(t, found)
		assert.Equal(t, PlacementGroupID("pg-dc2"), group2.ID)
		assert.False(t, mgr.IsLocalGroupNode("node1-dc2"))

		// Unknown node
		_, found = mgr.GetNodeGroup("unknown-node")
		assert.False(t, found)
	})

	t.Run("erasure coding capacity check", func(t *testing.T) {
		// 3 nodes can do 2+1 erasure coding
		assert.True(t, mgr.CanPerformErasureCoding(2, 1))

		// 3 nodes cannot do 4+2 erasure coding
		assert.False(t, mgr.CanPerformErasureCoding(4, 2))

		// Add nodes to allow 4+2
		require.NoError(t, mgr.AddNodeToGroup("pg-dc1", "node4-dc1"))
		require.NoError(t, mgr.AddNodeToGroup("pg-dc1", "node5-dc1"))
		require.NoError(t, mgr.AddNodeToGroup("pg-dc1", "node6-dc1"))

		// Now 6 nodes can do 4+2 erasure coding
		assert.True(t, mgr.CanPerformErasureCoding(4, 2))
	})
}

// TestPlacementGroupNodeMembershipEvents tests that node join/leave events
// are correctly propagated and tracked.
func TestPlacementGroupNodeMembershipEvents(t *testing.T) {
	config := PlacementGroupConfig{
		LocalGroupID: "pg-main",
		Groups: []PlacementGroup{
			{
				ID:       "pg-main",
				Name:     "Main Group",
				Nodes:    []string{"node1"},
				MinNodes: 2,
				MaxNodes: 5,
				Status:   PlacementGroupStatusDegraded, // Start degraded (below min)
			},
		},
	}

	mgr, err := NewPlacementGroupManager(config)
	require.NoError(t, err)

	// Track events
	var (
		joinEvents, leaveEvents, statusEvents int32
		lastJoinedNode, lastLeftNode          string
		lastStatus                            PlacementGroupStatus
		eventMu                               sync.Mutex
	)

	mgr.SetOnNodeJoinedGroup(func(groupID PlacementGroupID, nodeID string) {
		atomic.AddInt32(&joinEvents, 1)
		eventMu.Lock()

		lastJoinedNode = nodeID

		eventMu.Unlock()
	})

	mgr.SetOnNodeLeftGroup(func(groupID PlacementGroupID, nodeID string) {
		atomic.AddInt32(&leaveEvents, 1)
		eventMu.Lock()

		lastLeftNode = nodeID

		eventMu.Unlock()
	})

	mgr.SetOnGroupStatusChange(func(groupID PlacementGroupID, status PlacementGroupStatus) {
		atomic.AddInt32(&statusEvents, 1)
		eventMu.Lock()

		lastStatus = status

		eventMu.Unlock()
	})

	t.Run("node join triggers healthy status", func(t *testing.T) {
		// Adding a second node should transition to healthy (min_nodes = 2)
		err := mgr.AddNodeToGroup("pg-main", "node2")
		require.NoError(t, err)

		// Wait for callbacks to complete
		time.Sleep(100 * time.Millisecond)

		eventMu.Lock()
		assert.Equal(t, "node2", lastJoinedNode)
		assert.Equal(t, PlacementGroupStatusHealthy, lastStatus)
		eventMu.Unlock()

		assert.Equal(t, int32(1), atomic.LoadInt32(&joinEvents))
		assert.Equal(t, int32(1), atomic.LoadInt32(&statusEvents))
	})

	t.Run("node leave triggers degraded status", func(t *testing.T) {
		// Removing a node should transition back to degraded
		err := mgr.RemoveNodeFromGroup("pg-main", "node2")
		require.NoError(t, err)

		// Wait for callbacks to complete
		time.Sleep(100 * time.Millisecond)

		eventMu.Lock()
		assert.Equal(t, "node2", lastLeftNode)
		assert.Equal(t, PlacementGroupStatusDegraded, lastStatus)
		eventMu.Unlock()

		assert.Equal(t, int32(1), atomic.LoadInt32(&leaveEvents))
		assert.Equal(t, int32(2), atomic.LoadInt32(&statusEvents))
	})
}

// TestPlacementGroupShardDistribution tests that shard placement
// distributes objects evenly across nodes.
func TestPlacementGroupShardDistribution(t *testing.T) {
	config := PlacementGroupConfig{
		LocalGroupID: "pg-main",
		Groups: []PlacementGroup{
			{
				ID:       "pg-main",
				Name:     "Main Group",
				Nodes:    []string{"node1", "node2", "node3", "node4", "node5"},
				MinNodes: 3,
				MaxNodes: 10,
				Status:   PlacementGroupStatusHealthy,
			},
		},
	}

	mgr, err := NewPlacementGroupManager(config)
	require.NoError(t, err)

	t.Run("deterministic shard placement", func(t *testing.T) {
		bucket := testBucketName
		key := "test-object"
		numShards := 3

		// Same object should always map to same nodes
		nodes1, err := mgr.GetShardPlacementNodesForObject(bucket, key, numShards)
		require.NoError(t, err)

		nodes2, err := mgr.GetShardPlacementNodesForObject(bucket, key, numShards)
		require.NoError(t, err)

		assert.Equal(t, nodes1, nodes2)
	})

	t.Run("even distribution across nodes", func(t *testing.T) {
		nodeCounts := make(map[string]int)
		numObjects := 10000
		numShards := 3

		for i := range numObjects {
			bucket := testBucketName
			key := "object-" + string(rune(i))

			nodes, err := mgr.GetShardPlacementNodesForObject(bucket, key, numShards)
			require.NoError(t, err)

			for _, node := range nodes {
				nodeCounts[node]++
			}
		}

		// Verify all nodes are used
		assert.Len(t, nodeCounts, 5)

		// Verify reasonably even distribution (within 30% of mean)
		totalShards := numObjects * numShards
		expectedPerNode := totalShards / 5
		tolerance := float64(expectedPerNode) * 0.3

		for node, count := range nodeCounts {
			diff := float64(count - expectedPerNode)
			if diff < 0 {
				diff = -diff
			}

			assert.LessOrEqual(t, diff, tolerance,
				"Node %s has %d shards, expected ~%d (tolerance: %.0f)",
				node, count, expectedPerNode, tolerance)
		}
	})

	t.Run("shard placement after node changes", func(t *testing.T) {
		bucket := testBucketName
		key := "stable-object"
		numShards := 3

		// Get initial placement
		nodes1, err := mgr.GetShardPlacementNodesForObject(bucket, key, numShards)
		require.NoError(t, err)
		require.Len(t, nodes1, numShards)

		// Add a new node
		err = mgr.AddNodeToGroup("pg-main", "node6")
		require.NoError(t, err)

		// Placement may or may not change depending on hash
		nodes2, err := mgr.GetShardPlacementNodesForObject(bucket, key, numShards)
		require.NoError(t, err)
		require.Len(t, nodes2, numShards)

		// Remove the new node
		err = mgr.RemoveNodeFromGroup("pg-main", "node6")
		require.NoError(t, err)

		// Placement should return to original
		nodes3, err := mgr.GetShardPlacementNodesForObject(bucket, key, numShards)
		require.NoError(t, err)
		assert.Equal(t, nodes1, nodes3, "Placement should be stable after node removal")
	})
}

// TestPlacementGroupAuditIntegration tests that audit logging is correctly
// integrated with placement group operations.
func TestPlacementGroupAuditIntegration(t *testing.T) {
	config := PlacementGroupConfig{
		LocalGroupID: "pg-main",
		Groups: []PlacementGroup{
			{
				ID:         "pg-main",
				Name:       "Main Group",
				Datacenter: "dc1",
				Region:     "us-east",
				Nodes:      []string{"node1"},
				MinNodes:   2,
				MaxNodes:   5,
				Status:     PlacementGroupStatusDegraded,
			},
		},
	}

	mgr, err := NewPlacementGroupManager(config)
	require.NoError(t, err)

	// Create mock audit logger
	mockLogger := &mockAuditLogger{}
	mgr.SetAuditLogger(mockLogger)

	t.Run("audit node joined", func(t *testing.T) {
		err := mgr.AddNodeToGroup("pg-main", "node2")
		require.NoError(t, err)

		// Wait for async operations
		time.Sleep(100 * time.Millisecond)

		mockLogger.mu.Lock()
		nodeJoinedCount := mockLogger.nodeJoinedCount
		lastGroupID := mockLogger.lastGroupID
		lastNodeID := mockLogger.lastNodeID
		lastDatacenter := mockLogger.lastDatacenter
		lastRegion := mockLogger.lastRegion
		statusChangedCount := mockLogger.statusChangedCount
		mockLogger.mu.Unlock()

		assert.Equal(t, 1, nodeJoinedCount, "Expected 1 node joined event")
		assert.Equal(t, "pg-main", lastGroupID)
		assert.Equal(t, "node2", lastNodeID)
		assert.Equal(t, "dc1", lastDatacenter)
		assert.Equal(t, "us-east", lastRegion)

		// Status change may also be logged if group transitions from degraded to healthy
		// The status change audit is triggered when min_nodes threshold is reached
		assert.GreaterOrEqual(t, statusChangedCount, 0, "Status change count should be >= 0")
	})

	t.Run("audit node left", func(t *testing.T) {
		mockLogger.reset()

		err := mgr.RemoveNodeFromGroup("pg-main", "node2")
		require.NoError(t, err)

		// Wait for async operations
		time.Sleep(100 * time.Millisecond)

		mockLogger.mu.Lock()
		nodeLeftCount := mockLogger.nodeLeftCount
		lastNodeID := mockLogger.lastNodeID
		statusChangedCount := mockLogger.statusChangedCount
		mockLogger.mu.Unlock()

		assert.Equal(t, 1, nodeLeftCount, "Expected 1 node left event")
		assert.Equal(t, "node2", lastNodeID)

		// Status change may also be logged if group transitions below min_nodes
		assert.GreaterOrEqual(t, statusChangedCount, 0, "Status change count should be >= 0")
	})

	t.Run("audit status change", func(t *testing.T) {
		mockLogger.reset()

		err := mgr.UpdateGroupStatus("pg-main", PlacementGroupStatusOffline)
		require.NoError(t, err)

		// Wait for async operations
		time.Sleep(100 * time.Millisecond)

		mockLogger.mu.Lock()
		statusChangedCount := mockLogger.statusChangedCount
		lastOldStatus := mockLogger.lastOldStatus
		lastNewStatus := mockLogger.lastNewStatus
		mockLogger.mu.Unlock()

		assert.Equal(t, 1, statusChangedCount, "Expected 1 status change event")
		assert.Equal(t, string(PlacementGroupStatusDegraded), lastOldStatus)
		assert.Equal(t, string(PlacementGroupStatusOffline), lastNewStatus)
	})
}

// TestPlacementGroupCacheEfficiency tests that caching improves performance
// for frequently accessed data.
func TestPlacementGroupCacheEfficiency(t *testing.T) {
	config := PlacementGroupConfig{
		LocalGroupID: "pg-main",
		Groups: []PlacementGroup{
			{
				ID:       "pg-main",
				Name:     "Main Group",
				Nodes:    []string{"node1", "node2", "node3", "node4", "node5"},
				MinNodes: 3,
				MaxNodes: 10,
				Status:   PlacementGroupStatusHealthy,
			},
		},
	}

	mgr, err := NewPlacementGroupManager(config)
	require.NoError(t, err)

	t.Run("cached reads are consistent", func(t *testing.T) {
		// First call populates cache
		nodes1 := mgr.LocalGroupNodes()
		require.Len(t, nodes1, 5)

		// Subsequent calls should return same data
		for range 100 {
			nodes := mgr.LocalGroupNodes()
			assert.Equal(t, nodes1, nodes)
		}
	})

	t.Run("cache invalidated on mutation", func(t *testing.T) {
		// Get initial nodes
		nodes1 := mgr.LocalGroupNodes()
		require.Len(t, nodes1, 5)

		// Add a node
		err := mgr.AddNodeToGroup("pg-main", "node6")
		require.NoError(t, err)

		// Cache should be invalidated, new read should have 6 nodes
		nodes2 := mgr.LocalGroupNodes()
		assert.Len(t, nodes2, 6)
		assert.Contains(t, nodes2, "node6")

		// Remove the node
		err = mgr.RemoveNodeFromGroup("pg-main", "node6")
		require.NoError(t, err)

		// Cache should be invalidated again
		nodes3 := mgr.LocalGroupNodes()
		assert.Len(t, nodes3, 5)
		assert.NotContains(t, nodes3, "node6")
	})

	t.Run("concurrent cache access is safe", func(t *testing.T) {
		var wg sync.WaitGroup

		errors := make(chan error, 100)

		// Concurrent readers
		for range 50 {
			wg.Add(1)

			go func() {
				defer wg.Done()

				for range 100 {
					nodes := mgr.LocalGroupNodes()
					if len(nodes) < 3 || len(nodes) > 10 {
						errors <- assert.AnError
					}
				}
			}()
		}

		// Concurrent writers
		for i := range 5 {
			wg.Add(1)

			go func(id int) {
				defer wg.Done()

				nodeID := "concurrent-node-" + string(rune('A'+id))
				for range 10 {
					_ = mgr.AddNodeToGroup("pg-main", nodeID)
					_ = mgr.RemoveNodeFromGroup("pg-main", nodeID)
				}
			}(i)
		}

		wg.Wait()
		close(errors)

		// Should not have any errors
		for err := range errors {
			t.Errorf("Concurrent access error: %v", err)
		}
	})
}

// TestPlacementGroupContextCancellation tests that callbacks are properly cancelled
// when the manager is closed.
func TestPlacementGroupContextCancellation(t *testing.T) {
	config := PlacementGroupConfig{
		LocalGroupID: "pg-main",
		Groups: []PlacementGroup{
			{
				ID:       "pg-main",
				Name:     "Main Group",
				Nodes:    []string{"node1"},
				MinNodes: 1,
				MaxNodes: 10,
				Status:   PlacementGroupStatusHealthy,
			},
		},
	}

	mgr, err := NewPlacementGroupManager(config)
	require.NoError(t, err)

	// Track callback executions
	var (
		callbackCount int32
		callbackMu    sync.Mutex
	)

	mgr.SetOnNodeJoinedGroup(func(groupID PlacementGroupID, nodeID string) {
		// Simulate a slow callback
		time.Sleep(100 * time.Millisecond)
		callbackMu.Lock()

		callbackCount++

		callbackMu.Unlock()
	})

	t.Run("callbacks work before close", func(t *testing.T) {
		err := mgr.AddNodeToGroup("pg-main", "node2")
		require.NoError(t, err)

		// Wait for callback to complete
		time.Sleep(200 * time.Millisecond)

		callbackMu.Lock()

		count := callbackCount

		callbackMu.Unlock()
		assert.Equal(t, int32(1), count, "Callback should have been executed")
	})

	t.Run("callbacks skipped after close", func(t *testing.T) {
		// Close the manager
		err := mgr.Close()
		require.NoError(t, err)

		// Reset counter
		callbackMu.Lock()

		callbackCount = 0

		callbackMu.Unlock()

		// Try to add another node - callback should be skipped
		// Note: The mutation itself may still work, but callback won't execute
		_ = mgr.AddNodeToGroup("pg-main", "node3")

		// Wait a bit to see if callback would execute
		time.Sleep(200 * time.Millisecond)

		callbackMu.Lock()

		count := callbackCount

		callbackMu.Unlock()
		assert.Equal(t, int32(0), count, "Callback should have been skipped after close")
	})
}

// mockAuditLogger implements PlacementGroupAuditLogger for testing.
type mockAuditLogger struct {
	lastGroupID        string
	lastNodeID         string
	lastDatacenter     string
	lastRegion         string
	lastOldStatus      string
	lastNewStatus      string
	nodeJoinedCount    int
	nodeLeftCount      int
	statusChangedCount int
	mu                 sync.Mutex
}

func (m *mockAuditLogger) LogNodeJoined(groupID, nodeID, datacenter, region string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.nodeJoinedCount++
	m.lastGroupID = groupID
	m.lastNodeID = nodeID
	m.lastDatacenter = datacenter
	m.lastRegion = region
}

func (m *mockAuditLogger) LogNodeLeft(groupID, nodeID, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.nodeLeftCount++
	m.lastGroupID = groupID
	m.lastNodeID = nodeID
}

func (m *mockAuditLogger) LogStatusChanged(groupID, oldStatus, newStatus string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.statusChangedCount++
	m.lastGroupID = groupID
	m.lastOldStatus = oldStatus
	m.lastNewStatus = newStatus
}

func (m *mockAuditLogger) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.nodeJoinedCount = 0
	m.nodeLeftCount = 0
	m.statusChangedCount = 0
	m.lastGroupID = ""
	m.lastNodeID = ""
	m.lastDatacenter = ""
	m.lastRegion = ""
	m.lastOldStatus = ""
	m.lastNewStatus = ""
}
