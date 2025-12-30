package admin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/piwi3910/nebulaio/internal/cluster"
	"github.com/piwi3910/nebulaio/internal/metadata"
)

// Status constants for cluster members.
const (
	statusAlive = "alive"
)

// ClusterStore defines the interface for cluster operations.
type ClusterStore interface {
	IsLeader() bool
	LeaderAddress() (string, error)
	AddVoter(nodeID string, raftAddr string) error
	AddNonvoter(nodeID string, raftAddr string) error
	RemoveServer(nodeID string) error
	TransferLeadership(targetID, targetAddr string) error
	GetServers() (map[uint64]string, error)
	GetClusterConfiguration() (*metadata.ClusterConfiguration, error)
	Stats() map[string]string
	Snapshot() error
	LastIndex() (uint64, error)
	AppliedIndex() (uint64, error)
}

// ClusterHandler handles cluster-related admin API requests.
type ClusterHandler struct {
	discovery *cluster.Discovery
	store     ClusterStore
}

// NewClusterHandler creates a new cluster handler.
func NewClusterHandler(discovery *cluster.Discovery, store ClusterStore) *ClusterHandler {
	return &ClusterHandler{
		discovery: discovery,
		store:     store,
	}
}

// RegisterClusterRoutes registers cluster-related routes
// Note: Some cluster routes (/cluster/nodes, /cluster/nodes/{nodeId}/metrics) are registered
// in Handler with auth middleware, so they're not duplicated here.
func (h *ClusterHandler) RegisterClusterRoutes(r chi.Router) {
	r.Post("/cluster/nodes", h.AddNode)
	r.Delete("/cluster/nodes/{id}", h.RemoveNode)
	r.Get("/cluster/leader", h.GetLeader)
	r.Post("/cluster/transfer-leadership", h.TransferLeadership)
	r.Get("/cluster/config", h.GetRaftConfiguration)
	r.Get("/cluster/stats", h.GetRaftStats)
	r.Post("/cluster/snapshot", h.TriggerSnapshot)
	r.Get("/cluster/health", h.GetClusterHealth)
}

// NodeResponse represents a node in API responses.
type NodeResponse struct {
	JoinedAt   time.Time `json:"joined_at,omitempty"`
	LastSeen   time.Time `json:"last_seen,omitempty"`
	NodeID     string    `json:"node_id"`
	RaftAddr   string    `json:"raft_addr"`
	S3Addr     string    `json:"s3_addr"`
	AdminAddr  string    `json:"admin_addr"`
	GossipAddr string    `json:"gossip_addr,omitempty"`
	Role       string    `json:"role"`
	Version    string    `json:"version,omitempty"`
	Status     string    `json:"status"`
	IsLeader   bool      `json:"is_leader"`
	IsVoter    bool      `json:"is_voter"`
}

// ListNodesResponse represents the response for listing nodes.
type ListNodesResponse struct {
	LeaderID     string          `json:"leader_id"`
	Nodes        []*NodeResponse `json:"nodes"`
	TotalNodes   int             `json:"total_nodes"`
	HealthyNodes int             `json:"healthy_nodes"`
}

// ListNodes returns all cluster nodes.
func (h *ClusterHandler) ListNodes(w http.ResponseWriter, r *http.Request) {
	var (
		nodes        []*NodeResponse
		healthyCount int
	)

	// Get leader ID
	leaderID := ""
	if h.discovery != nil {
		leaderID = h.discovery.LeaderID()
	}

	// Get cluster configuration to determine voters
	voterMap := make(map[string]bool)

	if h.store != nil {
		config, err := h.store.GetClusterConfiguration()
		if err == nil {
			for _, server := range config.Servers {
				voterMap[strconv.FormatUint(server.ID, 10)] = server.IsVoter
			}
		}
	}

	// Get nodes from discovery
	if h.discovery != nil {
		for _, member := range h.discovery.Members() {
			isLeader := member.NodeID == leaderID
			isVoter := voterMap[member.NodeID]

			node := &NodeResponse{
				NodeID:     member.NodeID,
				RaftAddr:   member.RaftAddr,
				S3Addr:     member.S3Addr,
				AdminAddr:  member.AdminAddr,
				GossipAddr: member.GossipAddr,
				Role:       member.Role,
				Version:    member.Version,
				Status:     member.Status,
				IsLeader:   isLeader,
				IsVoter:    isVoter,
				JoinedAt:   member.JoinedAt,
				LastSeen:   member.LastSeen,
			}
			nodes = append(nodes, node)

			if member.Status == statusAlive {
				healthyCount++
			}
		}
	}

	response := &ListNodesResponse{
		Nodes:        nodes,
		TotalNodes:   len(nodes),
		HealthyNodes: healthyCount,
		LeaderID:     leaderID,
	}

	writeJSON(w, http.StatusOK, response)
}

// AddNodeRequest represents a request to add a node.
type AddNodeRequest struct {
	NodeID   string `json:"node_id"`
	RaftAddr string `json:"raft_addr"`
	AsVoter  bool   `json:"as_voter"` // true for voter, false for non-voter
}

// AddNode adds a new node to the cluster.
func (h *ClusterHandler) AddNode(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		writeError(w, "Store not initialized", http.StatusServiceUnavailable)
		return
	}

	if !h.store.IsLeader() {
		leaderAddr, _ := h.store.LeaderAddress()
		writeJSON(w, http.StatusTemporaryRedirect, map[string]string{
			"error":   "not leader",
			"leader":  leaderAddr,
			"message": "Request must be sent to the leader",
		})

		return
	}

	var req AddNodeRequest

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.NodeID == "" || req.RaftAddr == "" {
		writeError(w, "node_id and raft_addr are required", http.StatusBadRequest)
		return
	}

	if req.AsVoter {
		err = h.store.AddVoter(req.NodeID, req.RaftAddr)
	} else {
		err = h.store.AddNonvoter(req.NodeID, req.RaftAddr)
	}

	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Node added successfully",
		"node_id": req.NodeID,
	})
}

// RemoveNode removes a node from the cluster.
func (h *ClusterHandler) RemoveNode(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		writeError(w, "Store not initialized", http.StatusServiceUnavailable)
		return
	}

	if !h.store.IsLeader() {
		leaderAddr, _ := h.store.LeaderAddress()
		writeJSON(w, http.StatusTemporaryRedirect, map[string]string{
			"error":   "not leader",
			"leader":  leaderAddr,
			"message": "Request must be sent to the leader",
		})

		return
	}

	nodeID := chi.URLParam(r, "id")
	if nodeID == "" {
		writeError(w, "node_id is required", http.StatusBadRequest)
		return
	}

	err := h.store.RemoveServer(nodeID)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Node removed successfully",
		"node_id": nodeID,
	})
}

// LeaderResponse represents the leader information.
type LeaderResponse struct {
	LeaderID   string `json:"leader_id"`
	LeaderAddr string `json:"leader_addr"`
	IsLocal    bool   `json:"is_local"`
}

// GetLeader returns the current cluster leader.
func (h *ClusterHandler) GetLeader(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		writeError(w, "Store not initialized", http.StatusServiceUnavailable)
		return
	}

	leaderAddr, _ := h.store.LeaderAddress()
	leaderID := ""
	isLocal := h.store.IsLeader()

	if h.discovery != nil {
		leaderID = h.discovery.LeaderID()
	}

	response := &LeaderResponse{
		LeaderID:   leaderID,
		LeaderAddr: leaderAddr,
		IsLocal:    isLocal,
	}

	writeJSON(w, http.StatusOK, response)
}

// TransferLeadershipRequest represents a request to transfer leadership.
type TransferLeadershipRequest struct {
	TargetID string `json:"target_id"`
}

// TransferLeadership transfers leadership to another node.
func (h *ClusterHandler) TransferLeadership(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		writeError(w, "Store not initialized", http.StatusServiceUnavailable)
		return
	}

	if !h.store.IsLeader() {
		writeError(w, "Not the leader", http.StatusBadRequest)
		return
	}

	var req TransferLeadershipRequest

	decodeErr := json.NewDecoder(r.Body).Decode(&req)
	if decodeErr != nil {
		writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.TargetID == "" {
		writeError(w, "target_id is required", http.StatusBadRequest)
		return
	}

	// Get target address from configuration
	config, err := h.store.GetClusterConfiguration()
	if err != nil {
		writeError(w, "Failed to get configuration", http.StatusInternalServerError)
		return
	}

	var targetAddr string

	for _, server := range config.Servers {
		if strconv.FormatUint(server.ID, 10) == req.TargetID {
			targetAddr = server.Address
			break
		}
	}

	if targetAddr == "" {
		writeError(w, "Target node not found in cluster", http.StatusBadRequest)
		return
	}

	transferErr := h.store.TransferLeadership(req.TargetID, targetAddr)
	if transferErr != nil {
		writeError(w, transferErr.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message":   "Leadership transfer initiated",
		"target_id": req.TargetID,
	})
}

// RaftServerInfo represents a Raft server in the configuration.
type RaftServerInfo struct {
	ID       string `json:"id"`
	Address  string `json:"address"`
	Suffrage string `json:"suffrage"` // Voter, Nonvoter, Staging
}

// RaftConfigResponse represents the Raft configuration.
type RaftConfigResponse struct {
	Servers []RaftServerInfo `json:"servers"`
}

// GetRaftConfiguration returns the current Raft configuration.
func (h *ClusterHandler) GetRaftConfiguration(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		writeError(w, "Store not initialized", http.StatusServiceUnavailable)
		return
	}

	config, err := h.store.GetClusterConfiguration()
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	servers := make([]RaftServerInfo, 0, len(config.Servers))
	for _, server := range config.Servers {
		suffrage := "Nonvoter"
		if server.IsVoter {
			suffrage = "Voter"
		}

		servers = append(servers, RaftServerInfo{
			ID:       strconv.FormatUint(server.ID, 10),
			Address:  server.Address,
			Suffrage: suffrage,
		})
	}

	response := &RaftConfigResponse{
		Servers: servers,
	}

	writeJSON(w, http.StatusOK, response)
}

// GetRaftStats returns Raft statistics.
func (h *ClusterHandler) GetRaftStats(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		writeError(w, "Store not initialized", http.StatusServiceUnavailable)
		return
	}

	stats := h.store.Stats()
	writeJSON(w, http.StatusOK, stats)
}

// TriggerSnapshot triggers a manual Raft snapshot.
func (h *ClusterHandler) TriggerSnapshot(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		writeError(w, "Store not initialized", http.StatusServiceUnavailable)
		return
	}

	err := h.store.Snapshot()
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Snapshot triggered successfully",
	})
}

// ClusterHealthResponse represents the cluster health status.
type ClusterHealthResponse struct {
	State        string `json:"state"`
	LeaderID     string `json:"leader_id"`
	TotalNodes   int    `json:"total_nodes"`
	HealthyNodes int    `json:"healthy_nodes"`
	LastIndex    uint64 `json:"last_index"`
	AppliedIndex uint64 `json:"applied_index"`
	CommitIndex  uint64 `json:"commit_index"`
	Healthy      bool   `json:"healthy"`
	IsLeader     bool   `json:"is_leader"`
}

// GetClusterHealth returns the cluster health status.
func (h *ClusterHandler) GetClusterHealth(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		writeError(w, "Store not initialized", http.StatusServiceUnavailable)
		return
	}

	stats := h.store.Stats()
	state := stats["state"]
	isLeader := h.store.IsLeader()

	// Count healthy nodes
	var (
		totalNodes, healthyNodes int
		leaderID                 string
	)

	if h.discovery != nil {
		members := h.discovery.Members()

		totalNodes = len(members)
		for _, m := range members {
			if m.Status == statusAlive {
				healthyNodes++
			}
		}

		leaderID = h.discovery.LeaderID()
	}

	// Determine if cluster is healthy
	// Healthy if: has a leader, majority of nodes are alive
	healthy := leaderID != "" && healthyNodes > totalNodes/2

	lastIndex, _ := h.store.LastIndex()
	appliedIndex, _ := h.store.AppliedIndex()

	response := &ClusterHealthResponse{
		Healthy:      healthy,
		State:        state,
		IsLeader:     isLeader,
		LeaderID:     leaderID,
		TotalNodes:   totalNodes,
		HealthyNodes: healthyNodes,
		LastIndex:    lastIndex,
		AppliedIndex: appliedIndex,
	}

	statusCode := http.StatusOK
	if !healthy {
		statusCode = http.StatusServiceUnavailable
	}

	writeJSON(w, statusCode, response)
}

// NodeMetricsResponse represents metrics for a specific node.
type NodeMetricsResponse struct {
	JoinedAt      time.Time              `json:"joined_at"`
	LastHeartbeat time.Time              `json:"last_heartbeat"`
	Storage       map[string]interface{} `json:"storage,omitempty"`
	NodeID        string                 `json:"node_id"`
	Name          string                 `json:"name,omitempty"`
	Address       string                 `json:"address"`
	Role          string                 `json:"role"`
	Status        string                 `json:"status"`
	IsLeader      bool                   `json:"is_leader"`
	IsVoter       bool                   `json:"is_voter"`
}

// GetNodeMetrics returns metrics for a specific node.
func (h *ClusterHandler) GetNodeMetrics(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "nodeId")

	if h.discovery == nil {
		writeError(w, "Discovery not initialized", http.StatusServiceUnavailable)
		return
	}

	// Get leader ID
	leaderID := h.discovery.LeaderID()

	// Get cluster configuration to determine voters
	voterMap := make(map[string]bool)

	if h.store != nil {
		config, err := h.store.GetClusterConfiguration()
		if err == nil {
			for _, server := range config.Servers {
				voterMap[strconv.FormatUint(server.ID, 10)] = server.IsVoter
			}
		}
	}

	// Find the specific node
	var targetMember *cluster.NodeInfo

	for _, member := range h.discovery.Members() {
		if member.NodeID == nodeID {
			targetMember = member
			break
		}
	}

	if targetMember == nil {
		writeError(w, "Node not found", http.StatusNotFound)
		return
	}

	// Build metrics response
	response := &NodeMetricsResponse{
		NodeID:        targetMember.NodeID,
		Address:       targetMember.RaftAddr,
		Role:          targetMember.Role,
		Status:        targetMember.Status,
		IsLeader:      targetMember.NodeID == leaderID,
		IsVoter:       voterMap[targetMember.NodeID],
		JoinedAt:      targetMember.JoinedAt,
		LastHeartbeat: targetMember.LastSeen,
	}

	writeJSON(w, http.StatusOK, response)
}
