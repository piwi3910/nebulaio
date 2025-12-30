// Package rdma provides RDMA (Remote Direct Memory Access) transport for S3 operations.
// This enables ultra-low latency object storage access by bypassing the kernel network stack
// and enabling direct memory-to-memory transfers between client and server.
//
// Supported transports:
// - InfiniBand (highest performance)
// - RoCE v2 (RDMA over Converged Ethernet)
// - iWARP (Internet Wide Area RDMA Protocol)
package rdma

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// Transport type constants.
const (
	TransportInfiniBand = "infiniband"
	TransportRoCEv2     = "roce"
	TransportIWARP      = "iwarp"
	TransportAuto       = "auto" // Auto-detect best available
)

// Connection type constants.
const (
	ConnTypeRC = "RC" // Reliable Connection
	ConnTypeUD = "UD" // Unreliable Datagram
	ConnTypeUC = "UC" // Unreliable Connection
)

// Operation codes for S3 over RDMA.
const (
	OpGetObject uint8 = iota + 1
	OpPutObject
	OpDeleteObject
	OpHeadObject
	OpListObjects
	OpCreateMultipartUpload
	OpUploadPart
	OpCompleteMultipartUpload
	OpAbortMultipartUpload
)

var (
	ErrRDMANotAvailable   = errors.New("RDMA hardware not available")
	ErrConnectionFailed   = errors.New("RDMA connection failed")
	ErrMemoryRegistration = errors.New("memory registration failed")
	ErrQueuePairError     = errors.New("queue pair error")
	ErrCompletionError    = errors.New("completion queue error")
	ErrTimeout            = errors.New("RDMA operation timeout")
	ErrBufferTooSmall     = errors.New("buffer too small for operation")
	ErrNotConnected       = errors.New("not connected to remote endpoint")
)

// Config holds RDMA transport configuration.
type Config struct {
	Transport           string
	DeviceName          string
	HTTPSecretKey       string
	ConnectionType      string
	HTTPAccessKey       string
	HTTPEndpoint        string
	PreAllocateRegions  int
	SendQueueDepth      int
	MemoryPoolSize      int64
	MemoryRegionSize    int
	RecvQueueDepth      int
	ConnectionTimeout   time.Duration
	OperationTimeout    time.Duration
	CompletionQueueSize int
	Port                int
	MaxInlineData       int
	EnableZeroCopy      bool
	FallbackHTTP        bool
	EnableCompression   bool
	EnablePrefetch      bool
}

// DefaultConfig returns a default RDMA configuration.
func DefaultConfig() *Config {
	return &Config{
		Transport:           TransportAuto,
		Port:                1,
		ConnectionType:      ConnTypeRC,
		MaxInlineData:       256,
		SendQueueDepth:      128,
		RecvQueueDepth:      128,
		CompletionQueueSize: 256,
		MemoryPoolSize:      1 << 30, // 1GB
		MemoryRegionSize:    1 << 20, // 1MB
		PreAllocateRegions:  64,
		ConnectionTimeout:   10 * time.Second,
		OperationTimeout:    30 * time.Second,
		EnableZeroCopy:      true,
		EnablePrefetch:      false,
		FallbackHTTP:        true,
	}
}

// Transport represents an RDMA transport instance.
type Transport struct {
	config      *Config
	device      *Device
	protDomain  *ProtectionDomain
	memPool     *MemoryPool
	listener    *Listener
	metrics     *Metrics
	connections sync.Map
	mu          sync.RWMutex
	closed      atomic.Bool
}

// Device represents an RDMA-capable network device.
type Device struct {
	_handle       interface{}
	FirmwareVer   string
	NodeType      string
	Transport     string
	Name          string
	Ports         []PortInfo
	MaxInlineData int
	MaxQP         int
	MaxCQ         int
	MaxMR         int
	MaxPD         int
	MaxSGE        int
	GUID          uint64
	DeviceID      uint32
	VendorID      uint32
	AtomicCapable bool
}

// PortInfo represents information about a device port.
type PortInfo struct {
	State     string
	LinkLayer string
	Speed     string
	GID       []byte
	Number    int
	MTU       int
	LID       uint16
}

// ProtectionDomain isolates RDMA resources.
type ProtectionDomain struct {
	device  *Device
	_handle interface{} // Reserved for RDMA implementation
}

// MemoryRegion represents registered memory for RDMA operations.
type MemoryRegion struct {
	pool      *MemoryPool
	Buffer    []byte
	Address   uint64
	Length    uint64
	LocalKey  uint32
	RemoteKey uint32
	inUse     atomic.Bool
}

// MemoryPool manages pre-registered memory regions.
type MemoryPool struct {
	freeList   chan *MemoryRegion
	pd         *ProtectionDomain
	regions    []*MemoryRegion
	totalSize  int64
	usedSize   int64
	allocCount int64
	freeCount  int64
	mu         sync.Mutex
}

// QueuePair represents an RDMA queue pair for communication.
type QueuePair struct {
	_handle    interface{}
	SendCQ     *CompletionQueue
	RecvCQ     *CompletionQueue
	State      string
	MaxSend    int
	MaxRecv    int
	MaxSendSGE int
	MaxRecvSGE int
	Number     uint32
}

// CompletionQueue handles work request completions.
type CompletionQueue struct {
	_handle interface{}
	channel chan *WorkCompletion
	Size    int
	Count   int64
	Errors  int64
}

// WorkCompletion represents a completed work request.
type WorkCompletion struct {
	Timestamp time.Time
	Error     error
	ID        uint64
	Status    int
	ByteLen   uint32
	ImmData   uint32
	QueuePair uint32
	OpCode    uint8
}

// Connection represents an RDMA connection to a remote endpoint.
type Connection struct {
	LastActivity time.Time
	RecvMR       *MemoryRegion
	QueuePair    *QueuePair
	SendMR       *MemoryRegion
	transport    *Transport
	State        string
	RemoteAddr   string
	RemoteGID    []byte
	BytesSent    int64
	BytesRecv    int64
	Latency      time.Duration
	mu           sync.Mutex
	RemoteLID    uint16
	Connected    bool
}

// Listener accepts incoming RDMA connections.
type Listener struct {
	transport *Transport
	connChan  chan *Connection
	closeChan chan struct{}
	port      int
	accepting atomic.Bool
}

// Metrics tracks RDMA performance metrics.
type Metrics struct {
	mu                sync.RWMutex
	ConnectionsTotal  int64
	ConnectionsActive int64
	BytesSent         int64
	BytesReceived     int64
	OperationsTotal   int64
	OperationsSuccess int64
	OperationsFailed  int64
	LatencySum        time.Duration
	LatencyCount      int64
	MemoryUsed        int64
	MemoryTotal       int64
	CacheHits         int64
	CacheMisses       int64
}

// NewTransport creates a new RDMA transport.
func NewTransport(config *Config) (*Transport, error) {
	if config == nil {
		config = DefaultConfig()
	}

	t := &Transport{
		config:  config,
		metrics: &Metrics{},
	}

	// Try to initialize RDMA
	err := t.initRDMA()
	if err != nil {
		if config.FallbackHTTP {
			// RDMA not available, will fall back to HTTP
			return t, nil
		}

		return nil, fmt.Errorf("%w: %v", ErrRDMANotAvailable, err)
	}

	return t, nil
}

// initRDMA initializes the RDMA subsystem.
func (t *Transport) initRDMA() error {
	// Detect available RDMA devices
	devices, err := t.detectDevices()
	if err != nil {
		return err
	}

	if len(devices) == 0 {
		return ErrRDMANotAvailable
	}

	// Select the best device
	device := t.selectDevice(devices)
	if device == nil {
		return ErrRDMANotAvailable
	}

	t.device = device

	// Create protection domain
	pd, err := t.createProtectionDomain()
	if err != nil {
		return err
	}

	t.protDomain = pd

	// Create memory pool
	pool, err := t.createMemoryPool()
	if err != nil {
		return err
	}

	t.memPool = pool

	return nil
}

// detectDevices discovers available RDMA devices.
func (t *Transport) detectDevices() ([]*Device, error) {
	// In a real implementation, this would use libibverbs to enumerate devices
	// For now, return simulated devices for development/testing
	devices := []*Device{}

	// Check if we're in simulation mode or have actual hardware
	if t.config.DeviceName == "simulated" {
		devices = append(devices, &Device{
			Name:          "sim0",
			GUID:          0x0001020304050607,
			NodeType:      "CA",
			Transport:     TransportRoCEv2,
			VendorID:      0x15b3, // Mellanox
			MaxQP:         1024,
			MaxCQ:         1024,
			MaxMR:         16384,
			MaxPD:         256,
			MaxSGE:        32,
			MaxInlineData: 256,
			AtomicCapable: true,
			Ports: []PortInfo{
				{
					Number:    1,
					State:     "Active",
					MTU:       4096,
					LinkLayer: "Ethernet",
					Speed:     "100 Gbps",
				},
			},
		})
	}

	// TODO: Real implementation would call:
	// - ibv_get_device_list()
	// - ibv_open_device()
	// - ibv_query_device()
	// - ibv_query_port()

	return devices, nil
}

// selectDevice chooses the best available device.
func (t *Transport) selectDevice(devices []*Device) *Device {
	if len(devices) == 0 {
		return nil
	}

	// If specific device requested, find it
	if t.config.DeviceName != "" && t.config.DeviceName != "simulated" {
		for _, d := range devices {
			if d.Name == t.config.DeviceName {
				return d
			}
		}

		return nil
	}

	// Select based on transport preference
	switch t.config.Transport {
	case TransportInfiniBand:
		for _, d := range devices {
			if d.Transport == TransportInfiniBand {
				return d
			}
		}
	case TransportRoCEv2:
		for _, d := range devices {
			if d.Transport == TransportRoCEv2 {
				return d
			}
		}
	case TransportIWARP:
		for _, d := range devices {
			if d.Transport == TransportIWARP {
				return d
			}
		}
	}

	// Auto-select: prefer InfiniBand > RoCE > iWARP
	priority := map[string]int{
		TransportInfiniBand: 3,
		TransportRoCEv2:     2,
		TransportIWARP:      1,
	}

	var best *Device

	bestPriority := 0
	for _, d := range devices {
		if p := priority[d.Transport]; p > bestPriority {
			best = d
			bestPriority = p
		}
	}

	return best
}

// createProtectionDomain creates an RDMA protection domain.
func (t *Transport) createProtectionDomain() (*ProtectionDomain, error) {
	if t.device == nil {
		return nil, ErrRDMANotAvailable
	}

	// TODO: Real implementation would call ibv_alloc_pd()

	return &ProtectionDomain{
		device: t.device,
	}, nil
}

// createMemoryPool creates the pre-registered memory pool.
func (t *Transport) createMemoryPool() (*MemoryPool, error) {
	if t.protDomain == nil {
		return nil, ErrRDMANotAvailable
	}

	pool := &MemoryPool{
		pd:        t.protDomain,
		totalSize: t.config.MemoryPoolSize,
		freeList:  make(chan *MemoryRegion, t.config.PreAllocateRegions),
	}

	// Pre-allocate memory regions
	for range t.config.PreAllocateRegions {
		mr, err := pool.allocateRegion(t.config.MemoryRegionSize)
		if err != nil {
			return nil, err
		}

		pool.regions = append(pool.regions, mr)
		pool.freeList <- mr
	}

	t.metrics.MemoryTotal = pool.totalSize

	return pool, nil
}

// allocateRegion allocates and registers a memory region.
func (p *MemoryPool) allocateRegion(size int) (*MemoryRegion, error) {
	// Allocate aligned memory for DMA
	buffer := make([]byte, size)

	// TODO: Real implementation would:
	// 1. Allocate page-aligned memory
	// 2. Call ibv_reg_mr() to register with RDMA NIC
	// 3. Get local/remote keys for RDMA operations

	//nolint:gosec // G115: Region count and size are within safe bounds for simulated values
	mr := &MemoryRegion{
		Buffer:    buffer,
		LocalKey:  uint32(len(p.regions)), // Simulated
		RemoteKey: uint32(len(p.regions)), // Simulated
		Address:   0,                      // Would be real address
		Length:    uint64(size),
		pool:      p,
	}

	return mr, nil
}

// GetRegion gets a memory region from the pool.
func (p *MemoryPool) GetRegion(ctx context.Context) (*MemoryRegion, error) {
	select {
	case mr := <-p.freeList:
		mr.inUse.Store(true)
		atomic.AddInt64(&p.usedSize, int64(len(mr.Buffer)))
		atomic.AddInt64(&p.allocCount, 1)

		return mr, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ReleaseRegion returns a memory region to the pool.
func (p *MemoryPool) ReleaseRegion(mr *MemoryRegion) {
	if mr == nil || !mr.inUse.Load() {
		return
	}

	// Clear sensitive data
	for i := range mr.Buffer {
		mr.Buffer[i] = 0
	}

	mr.inUse.Store(false)
	atomic.AddInt64(&p.usedSize, -int64(len(mr.Buffer)))
	atomic.AddInt64(&p.freeCount, 1)

	select {
	case p.freeList <- mr:
	default:
		// Pool is full, this shouldn't happen with proper sizing
	}
}

// IsAvailable checks if RDMA is available.
func (t *Transport) IsAvailable() bool {
	return t.device != nil && t.protDomain != nil
}

// Connect establishes an RDMA connection to a remote endpoint.
func (t *Transport) Connect(ctx context.Context, addr string) (*Connection, error) {
	if !t.IsAvailable() {
		return nil, ErrRDMANotAvailable
	}

	// Check if already connected
	if conn, ok := t.connections.Load(addr); ok {
		c := conn.(*Connection)
		if c.Connected {
			return c, nil
		}
	}

	conn := &Connection{
		RemoteAddr: addr,
		State:      "Connecting",
		transport:  t,
	}

	// Create queue pair
	qp, err := t.createQueuePair()
	if err != nil {
		return nil, err
	}

	conn.QueuePair = qp

	// Allocate send/receive buffers
	sendMR, err := t.memPool.GetRegion(ctx)
	if err != nil {
		return nil, err
	}

	conn.SendMR = sendMR

	recvMR, err := t.memPool.GetRegion(ctx)
	if err != nil {
		t.memPool.ReleaseRegion(sendMR)
		return nil, err
	}

	conn.RecvMR = recvMR

	// Exchange connection info with remote (simulated)
	// Real implementation would use a side-channel (TCP) for this exchange
	if err := t.exchangeConnectionInfo(ctx, conn); err != nil {
		t.memPool.ReleaseRegion(sendMR)
		t.memPool.ReleaseRegion(recvMR)

		return nil, err
	}

	// Transition QP to Ready-to-Send state
	if err := t.transitionQPToRTS(conn.QueuePair); err != nil {
		t.memPool.ReleaseRegion(sendMR)
		t.memPool.ReleaseRegion(recvMR)

		return nil, err
	}

	conn.Connected = true
	conn.State = "Connected"
	conn.LastActivity = time.Now()

	t.connections.Store(addr, conn)
	atomic.AddInt64(&t.metrics.ConnectionsTotal, 1)
	atomic.AddInt64(&t.metrics.ConnectionsActive, 1)

	return conn, nil
}

// createQueuePair creates an RDMA queue pair.
func (t *Transport) createQueuePair() (*QueuePair, error) {
	// Create completion queues
	sendCQ := &CompletionQueue{
		Size:    t.config.CompletionQueueSize,
		channel: make(chan *WorkCompletion, t.config.CompletionQueueSize),
	}
	recvCQ := &CompletionQueue{
		Size:    t.config.CompletionQueueSize,
		channel: make(chan *WorkCompletion, t.config.CompletionQueueSize),
	}

	// TODO: Real implementation would call:
	// - ibv_create_cq() for each CQ
	// - ibv_create_qp() with appropriate attributes

	qp := &QueuePair{
		Number:     0, // Would be assigned by ibv_create_qp
		State:      "RESET",
		SendCQ:     sendCQ,
		RecvCQ:     recvCQ,
		MaxSend:    t.config.SendQueueDepth,
		MaxRecv:    t.config.RecvQueueDepth,
		MaxSendSGE: 4,
		MaxRecvSGE: 4,
	}

	return qp, nil
}

// exchangeConnectionInfo exchanges RDMA connection info with remote.
func (t *Transport) exchangeConnectionInfo(ctx context.Context, conn *Connection) error {
	// In a real implementation, this would:
	// 1. Get local GID/LID from the port
	// 2. Get local QP number
	// 3. Exchange this info with remote via TCP socket
	// 4. Store remote's GID/LID/QPN

	// For now, simulate the exchange
	conn.RemoteGID = make([]byte, 16)
	conn.RemoteLID = 1

	return nil
}

// transitionQPToRTS transitions QP through RESET -> INIT -> RTR -> RTS.
func (t *Transport) transitionQPToRTS(qp *QueuePair) error {
	// TODO: Real implementation would call ibv_modify_qp() for each transition
	states := []string{"INIT", "RTR", "RTS"}
	for _, state := range states {
		qp.State = state
	}

	return nil
}

// Disconnect closes an RDMA connection.
func (t *Transport) Disconnect(addr string) error {
	conn, ok := t.connections.LoadAndDelete(addr)
	if !ok {
		return nil
	}

	c := conn.(*Connection)

	c.mu.Lock()
	defer c.mu.Unlock()

	c.Connected = false
	c.State = "Disconnected"

	// Release memory regions
	if c.SendMR != nil {
		t.memPool.ReleaseRegion(c.SendMR)
	}

	if c.RecvMR != nil {
		t.memPool.ReleaseRegion(c.RecvMR)
	}

	// TODO: Destroy QP and CQs

	atomic.AddInt64(&t.metrics.ConnectionsActive, -1)

	return nil
}

// Listen starts accepting incoming RDMA connections.
func (t *Transport) Listen(port int) (*Listener, error) {
	if !t.IsAvailable() {
		return nil, ErrRDMANotAvailable
	}

	l := &Listener{
		transport: t,
		port:      port,
		connChan:  make(chan *Connection, 16),
		closeChan: make(chan struct{}),
	}

	l.accepting.Store(true)
	t.listener = l

	// TODO: Real implementation would set up RDMA CM listener

	return l, nil
}

// Accept accepts an incoming connection.
func (l *Listener) Accept(ctx context.Context) (*Connection, error) {
	if !l.accepting.Load() {
		return nil, errors.New("listener closed")
	}

	select {
	case conn := <-l.connChan:
		return conn, nil
	case <-l.closeChan:
		return nil, errors.New("listener closed")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Close closes the listener.
func (l *Listener) Close() error {
	if l.accepting.CompareAndSwap(true, false) {
		close(l.closeChan)
	}

	return nil
}

// Close closes the transport and releases all resources.
func (t *Transport) Close() error {
	if !t.closed.CompareAndSwap(false, true) {
		return nil
	}

	// Close listener
	if t.listener != nil {
		_ = t.listener.Close()
	}

	// Close all connections
	t.connections.Range(func(key, value interface{}) bool {
		_ = t.Disconnect(key.(string))
		return true
	})

	// Release memory pool
	if t.memPool != nil {
		// Deregister all memory regions
		for _, mr := range t.memPool.regions {
			// TODO: ibv_dereg_mr()
			_ = mr
		}
	}

	// Deallocate protection domain
	// TODO: ibv_dealloc_pd() when RDMA is fully implemented
	_ = t.protDomain // Mark as intentionally unused for now

	// Close device
	// TODO: ibv_close_device() when RDMA is fully implemented
	_ = t.device // Mark as intentionally unused for now

	return nil
}

// GetMetrics returns current RDMA metrics.
func (t *Transport) GetMetrics() *Metrics {
	t.metrics.mu.RLock()
	defer t.metrics.mu.RUnlock()

	// Return a copy
	m := &Metrics{
		ConnectionsTotal:  atomic.LoadInt64(&t.metrics.ConnectionsTotal),
		ConnectionsActive: atomic.LoadInt64(&t.metrics.ConnectionsActive),
		BytesSent:         atomic.LoadInt64(&t.metrics.BytesSent),
		BytesReceived:     atomic.LoadInt64(&t.metrics.BytesReceived),
		OperationsTotal:   atomic.LoadInt64(&t.metrics.OperationsTotal),
		OperationsSuccess: atomic.LoadInt64(&t.metrics.OperationsSuccess),
		OperationsFailed:  atomic.LoadInt64(&t.metrics.OperationsFailed),
		LatencySum:        t.metrics.LatencySum,
		LatencyCount:      t.metrics.LatencyCount,
		MemoryUsed:        atomic.LoadInt64(&t.metrics.MemoryUsed),
		MemoryTotal:       atomic.LoadInt64(&t.metrics.MemoryTotal),
		CacheHits:         atomic.LoadInt64(&t.metrics.CacheHits),
		CacheMisses:       atomic.LoadInt64(&t.metrics.CacheMisses),
	}

	if t.memPool != nil {
		m.MemoryUsed = atomic.LoadInt64(&t.memPool.usedSize)
	}

	return m
}

// AverageLatency returns the average operation latency.
func (m *Metrics) AverageLatency() time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.LatencyCount == 0 {
		return 0
	}

	return m.LatencySum / time.Duration(m.LatencyCount)
}

// S3Request represents an S3 operation request over RDMA.
type S3Request struct {
	Body        io.Reader
	Metadata    map[string]string
	LocalBuffer *MemoryRegion
	Bucket      string
	Key         string
	VersionID   string
	ContentType string
	BodySize    int64
	RemoteAddr  uint64
	RemoteKey   uint32
	OpCode      uint8
}

// S3Response represents an S3 operation response over RDMA.
type S3Response struct {
	Body             io.Reader
	Metadata         map[string]string
	ContentType      string
	ETag             string
	StatusCode       int
	BodySize         int64
	BytesTransferred int64
	Latency          time.Duration
}

// Execute executes an S3 request over RDMA.
func (c *Connection) Execute(ctx context.Context, req *S3Request) (*S3Response, error) {
	if !c.Connected {
		return nil, ErrNotConnected
	}

	start := time.Now()

	c.mu.Lock()
	defer c.mu.Unlock()

	// Update activity
	c.LastActivity = time.Now()

	// Serialize request
	reqData, err := c.serializeRequest(req)
	if err != nil {
		return nil, err
	}

	// Copy to send buffer
	if len(reqData) > len(c.SendMR.Buffer) {
		return nil, ErrBufferTooSmall
	}

	copy(c.SendMR.Buffer, reqData)

	// Post send work request
	if err := c.postSend(ctx, len(reqData)); err != nil {
		return nil, err
	}

	// Wait for send completion
	if err := c.waitCompletion(ctx, c.QueuePair.SendCQ); err != nil {
		return nil, err
	}

	// Post receive for response
	if err := c.postReceive(ctx); err != nil {
		return nil, err
	}

	// Wait for response
	wc, err := c.waitReceive(ctx)
	if err != nil {
		return nil, err
	}

	// Deserialize response
	resp, err := c.deserializeResponse(c.RecvMR.Buffer[:wc.ByteLen])
	if err != nil {
		return nil, err
	}

	resp.Latency = time.Since(start)
	resp.BytesTransferred = int64(len(reqData) + int(wc.ByteLen))

	// Update metrics
	atomic.AddInt64(&c.BytesSent, int64(len(reqData)))
	atomic.AddInt64(&c.BytesRecv, int64(wc.ByteLen))
	c.Latency = resp.Latency

	t := c.transport
	atomic.AddInt64(&t.metrics.BytesSent, int64(len(reqData)))
	atomic.AddInt64(&t.metrics.BytesReceived, int64(wc.ByteLen))
	atomic.AddInt64(&t.metrics.OperationsTotal, 1)
	atomic.AddInt64(&t.metrics.OperationsSuccess, 1)
	t.metrics.mu.Lock()
	t.metrics.LatencySum += resp.Latency
	t.metrics.LatencyCount++
	t.metrics.mu.Unlock()

	return resp, nil
}

// serializeRequest serializes an S3 request for RDMA transmission.
func (c *Connection) serializeRequest(req *S3Request) ([]byte, error) {
	// Simple binary format:
	// [OpCode:1][BucketLen:2][KeyLen:2][BodyLen:8][Bucket][Key][Body]
	bucketBytes := []byte(req.Bucket)
	keyBytes := []byte(req.Key)

	size := 1 + 2 + 2 + 8 + len(bucketBytes) + len(keyBytes)
	if req.Body != nil && req.BodySize > 0 {
		size += int(req.BodySize)
	}

	buf := make([]byte, size)
	offset := 0

	// OpCode
	buf[offset] = req.OpCode
	offset++

	// Bucket length and data
	buf[offset] = byte(len(bucketBytes) >> 8)
	buf[offset+1] = byte(len(bucketBytes))
	offset += 2
	copy(buf[offset:], bucketBytes)
	offset += len(bucketBytes)

	// Key length and data
	buf[offset] = byte(len(keyBytes) >> 8)
	buf[offset+1] = byte(len(keyBytes))
	offset += 2
	copy(buf[offset:], keyBytes)
	offset += len(keyBytes)

	// Body length
	for i := 7; i >= 0; i-- {
		buf[offset+i] = byte(req.BodySize >> (8 * (7 - i)))
	}

	offset += 8

	// Body data
	if req.Body != nil && req.BodySize > 0 {
		_, err := io.ReadFull(req.Body, buf[offset:offset+int(req.BodySize)])
		if err != nil {
			return nil, err
		}
	}

	return buf, nil
}

// deserializeResponse deserializes an S3 response from RDMA buffer.
func (c *Connection) deserializeResponse(data []byte) (*S3Response, error) {
	if len(data) < 3 {
		return nil, errors.New("response too short")
	}

	resp := &S3Response{}

	// Status code (2 bytes)
	resp.StatusCode = int(data[0])<<8 | int(data[1])

	// Body length (8 bytes)
	if len(data) >= 10 {
		var bodyLen int64
		for i := range 8 {
			bodyLen = bodyLen<<8 | int64(data[2+i])
		}

		resp.BodySize = bodyLen

		// Body data
		if bodyLen > 0 && len(data) >= 10+int(bodyLen) {
			resp.Body = io.NopCloser(
				io.NewSectionReader(
					&bytesReaderAt{data: data[10 : 10+int(bodyLen)]},
					0,
					bodyLen,
				),
			)
		}
	}

	return resp, nil
}

// bytesReaderAt implements io.ReaderAt for a byte slice.
type bytesReaderAt struct {
	data []byte
}

func (r *bytesReaderAt) ReadAt(p []byte, off int64) (n int, err error) {
	if off >= int64(len(r.data)) {
		return 0, io.EOF
	}

	n = copy(p, r.data[off:])
	if n < len(p) {
		err = io.EOF
	}

	return
}

// postSend posts a send work request.
func (c *Connection) postSend(ctx context.Context, length int) error {
	// TODO: Real implementation would call ibv_post_send()
	return nil
}

// postReceive posts a receive work request.
func (c *Connection) postReceive(ctx context.Context) error {
	// TODO: Real implementation would call ibv_post_recv()
	return nil
}

// waitCompletion waits for a work completion.
func (c *Connection) waitCompletion(ctx context.Context, cq *CompletionQueue) error {
	// TODO: Real implementation would poll/wait on CQ
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(c.transport.config.OperationTimeout):
		return ErrTimeout
	default:
		// Simulated immediate completion
		return nil
	}
}

// waitReceive waits for a receive completion.
func (c *Connection) waitReceive(ctx context.Context) (*WorkCompletion, error) {
	// TODO: Real implementation would poll receive CQ
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(c.transport.config.OperationTimeout):
		return nil, ErrTimeout
	default:
		// Simulated response
		return &WorkCompletion{
			Status:    0,
			ByteLen:   0,
			Timestamp: time.Now(),
		}, nil
	}
}
