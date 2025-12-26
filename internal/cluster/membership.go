package cluster

import (
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/memberlist"
	"github.com/rs/zerolog/log"
)

// MembershipConfig holds configuration for the membership service
type MembershipConfig struct {
	NodeID        string
	BindAddr      string
	BindPort      int
	AdvertiseAddr string
	AdvertisePort int
	NodeMeta      []byte
}

// NodeEventType represents the type of node event
type NodeEventType int

const (
	NodeJoin NodeEventType = iota
	NodeLeave
	NodeUpdate
)

// NodeEvent represents an event for a node
type NodeEvent struct {
	Type   NodeEventType
	NodeID string
	Meta   []byte
}

// Membership manages cluster membership using hashicorp/memberlist
type Membership struct {
	config   MembershipConfig
	list     *memberlist.Memberlist
	events   chan NodeEvent
	delegate *memberDelegate

	onJoin   func(nodeID string, meta []byte)
	onLeave  func(nodeID string)
	onUpdate func(nodeID string, meta []byte)

	mu sync.RWMutex
}

// memberDelegate implements memberlist.Delegate
type memberDelegate struct {
	meta       []byte
	broadcasts *memberlist.TransmitLimitedQueue
	mu         sync.RWMutex
}

// NodeMeta returns the local node's metadata
func (d *memberDelegate) NodeMeta(limit int) []byte {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.meta
}

// NotifyMsg is called when a user-data message is received
func (d *memberDelegate) NotifyMsg(b []byte) {
	// We're not using direct messages for now
}

// GetBroadcasts returns a slice of messages to broadcast
func (d *memberDelegate) GetBroadcasts(overhead, limit int) [][]byte {
	if d.broadcasts == nil {
		return nil
	}
	return d.broadcasts.GetBroadcasts(overhead, limit)
}

// LocalState returns the local state for state exchange
func (d *memberDelegate) LocalState(join bool) []byte {
	return nil
}

// MergeRemoteState merges remote state during state exchange
func (d *memberDelegate) MergeRemoteState(buf []byte, join bool) {
	// No state merging needed for now
}

// UpdateMeta updates the node metadata
func (d *memberDelegate) UpdateMeta(meta []byte) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.meta = meta
}

// eventDelegate implements memberlist.EventDelegate
type eventDelegate struct {
	membership *Membership
}

// NotifyJoin is called when a node joins
func (e *eventDelegate) NotifyJoin(node *memberlist.Node) {
	if node.Name == e.membership.config.NodeID {
		return
	}

	log.Debug().
		Str("node", node.Name).
		Str("addr", node.Address()).
		Msg("Memberlist: node joined")

	if e.membership.onJoin != nil {
		e.membership.onJoin(node.Name, node.Meta)
	}
}

// NotifyLeave is called when a node leaves
func (e *eventDelegate) NotifyLeave(node *memberlist.Node) {
	if node.Name == e.membership.config.NodeID {
		return
	}

	log.Debug().
		Str("node", node.Name).
		Msg("Memberlist: node left")

	if e.membership.onLeave != nil {
		e.membership.onLeave(node.Name)
	}
}

// NotifyUpdate is called when a node's metadata is updated
func (e *eventDelegate) NotifyUpdate(node *memberlist.Node) {
	if node.Name == e.membership.config.NodeID {
		return
	}

	log.Debug().
		Str("node", node.Name).
		Msg("Memberlist: node updated")

	if e.membership.onUpdate != nil {
		e.membership.onUpdate(node.Name, node.Meta)
	}
}

// NewMembership creates a new Membership instance
func NewMembership(
	config MembershipConfig,
	onJoin func(nodeID string, meta []byte),
	onLeave func(nodeID string),
	onUpdate func(nodeID string, meta []byte),
) (*Membership, error) {
	m := &Membership{
		config:   config,
		events:   make(chan NodeEvent, 100),
		onJoin:   onJoin,
		onLeave:  onLeave,
		onUpdate: onUpdate,
	}

	// Create delegate
	m.delegate = &memberDelegate{
		meta: config.NodeMeta,
	}

	// Configure memberlist
	mlConfig := memberlist.DefaultLANConfig()
	mlConfig.Name = config.NodeID
	mlConfig.BindAddr = config.BindAddr
	mlConfig.BindPort = config.BindPort
	mlConfig.AdvertiseAddr = config.AdvertiseAddr
	mlConfig.AdvertisePort = config.AdvertisePort
	mlConfig.Delegate = m.delegate
	mlConfig.Events = &eventDelegate{membership: m}

	// Reduce logging
	mlConfig.LogOutput = &memberlistLogAdapter{}

	// Tune for faster failure detection in development
	mlConfig.GossipInterval = 200 * time.Millisecond
	mlConfig.ProbeInterval = 1 * time.Second
	mlConfig.ProbeTimeout = 500 * time.Millisecond
	mlConfig.SuspicionMult = 4
	mlConfig.PushPullInterval = 15 * time.Second
	mlConfig.GossipNodes = 3

	// Create broadcast queue
	m.delegate.broadcasts = &memberlist.TransmitLimitedQueue{
		NumNodes: func() int {
			if m.list == nil {
				return 1
			}
			return m.list.NumMembers()
		},
		RetransmitMult: 3,
	}

	// Create memberlist
	list, err := memberlist.Create(mlConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create memberlist: %w", err)
	}
	m.list = list

	log.Info().
		Str("node_id", config.NodeID).
		Str("bind_addr", fmt.Sprintf("%s:%d", config.BindAddr, config.BindPort)).
		Str("advertise_addr", fmt.Sprintf("%s:%d", config.AdvertiseAddr, config.AdvertisePort)).
		Msg("Memberlist initialized")

	return m, nil
}

// Join joins an existing cluster
func (m *Membership) Join(existing []string) error {
	if len(existing) == 0 {
		return nil
	}

	log.Info().Strs("addresses", existing).Msg("Joining cluster")

	n, err := m.list.Join(existing)
	if err != nil {
		return fmt.Errorf("failed to join cluster: %w", err)
	}

	log.Info().Int("contacted_nodes", n).Msg("Joined cluster")
	return nil
}

// Leave gracefully leaves the cluster
func (m *Membership) Leave(timeout time.Duration) error {
	log.Info().Msg("Leaving cluster")

	if err := m.list.Leave(timeout); err != nil {
		return fmt.Errorf("failed to leave cluster: %w", err)
	}

	if err := m.list.Shutdown(); err != nil {
		return fmt.Errorf("failed to shutdown memberlist: %w", err)
	}

	return nil
}

// Members returns all cluster members
func (m *Membership) Members() []*memberlist.Node {
	return m.list.Members()
}

// NumMembers returns the number of cluster members
func (m *Membership) NumMembers() int {
	return m.list.NumMembers()
}

// LocalNode returns the local node
func (m *Membership) LocalNode() *memberlist.Node {
	return m.list.LocalNode()
}

// UpdateMeta updates the local node's metadata
func (m *Membership) UpdateMeta(meta []byte) error {
	m.delegate.UpdateMeta(meta)
	return m.list.UpdateNode(10 * time.Second)
}

// HealthScore returns the health score of the local node
// Lower is better (0 = healthy)
func (m *Membership) HealthScore() int {
	return m.list.GetHealthScore()
}

// SendReliable sends a message to a specific node reliably
func (m *Membership) SendReliable(node *memberlist.Node, msg []byte) error {
	return m.list.SendReliable(node, msg)
}

// memberlistLogAdapter adapts memberlist logging to zerolog
type memberlistLogAdapter struct{}

func (l *memberlistLogAdapter) Write(p []byte) (n int, err error) {
	// Filter out verbose memberlist logs
	log.Trace().Str("source", "memberlist").Msg(string(p))
	return len(p), nil
}

// Broadcast represents a message to broadcast to the cluster
type Broadcast struct {
	msg    []byte
	notify chan<- struct{}
}

// Invalidates checks if this broadcast invalidates another
func (b *Broadcast) Invalidates(other memberlist.Broadcast) bool {
	return false
}

// Message returns the message to broadcast
func (b *Broadcast) Message() []byte {
	return b.msg
}

// Finished is called when the broadcast is complete
func (b *Broadcast) Finished() {
	if b.notify != nil {
		close(b.notify)
	}
}

// QueueBroadcast queues a message for broadcast to all nodes
func (m *Membership) QueueBroadcast(msg []byte) {
	b := &Broadcast{
		msg:    msg,
		notify: nil,
	}
	m.delegate.broadcasts.QueueBroadcast(b)
}

// QueueBroadcastNotify queues a message and notifies when complete
func (m *Membership) QueueBroadcastNotify(msg []byte) <-chan struct{} {
	notify := make(chan struct{})
	b := &Broadcast{
		msg:    msg,
		notify: notify,
	}
	m.delegate.broadcasts.QueueBroadcast(b)
	return notify
}
