// Package rdma provides the libibverbs abstraction layer for RDMA hardware integration.
//
// This file defines the interface between NebulaIO's RDMA transport and the
// underlying RDMA hardware. It provides:
// - Hardware abstraction for different RDMA implementations
// - CGo bindings interface for libibverbs (when built with hardware support)
// - Simulated mode for development and testing
//
// Build Tags:
// - Default: Uses simulated backend (no hardware required)
// - rdma_hw: Uses actual libibverbs bindings (requires RDMA hardware)
//
// To build with hardware support:
//
//	go build -tags rdma_hw ./...
package rdma

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Verbs errors.
var (
	ErrVerbsNotInitialized = errors.New("verbs not initialized")
	ErrDeviceNotFound      = errors.New("RDMA device not found")
	ErrContextCreation     = errors.New("failed to create device context")
	ErrPDCreation          = errors.New("failed to create protection domain")
	ErrCQCreation          = errors.New("failed to create completion queue")
	ErrQPCreation          = errors.New("failed to create queue pair")
	ErrMRCreation          = errors.New("failed to create memory region")
	ErrPostSend            = errors.New("failed to post send request")
	ErrPostRecv            = errors.New("failed to post receive request")
	ErrPollCQ              = errors.New("failed to poll completion queue")
	ErrModifyQP            = errors.New("failed to modify queue pair state")
)

// VerbsBackend defines the interface for RDMA verbs operations.
// This abstraction allows switching between simulated and hardware backends.
type VerbsBackend interface {
	// Initialization
	Init() error
	Close() error

	// Device Management
	GetDeviceList() ([]VerbsDeviceInfo, error)
	OpenDevice(name string) (VerbsContext, error)
	CloseDevice(ctx VerbsContext) error

	// Protection Domain
	AllocPD(ctx VerbsContext) (VerbsPD, error)
	DeallocPD(pd VerbsPD) error

	// Completion Queue
	CreateCQ(ctx VerbsContext, cqe int) (VerbsCQ, error)
	DestroyCQ(cq VerbsCQ) error
	PollCQ(cq VerbsCQ, numEntries int) ([]VerbsWorkCompletion, error)

	// Queue Pair
	CreateQP(pd VerbsPD, sendCQ, recvCQ VerbsCQ, qpType QPType, maxSend, maxRecv, maxSge int) (VerbsQP, error)
	DestroyQP(qp VerbsQP) error
	ModifyQPToInit(qp VerbsQP, port int) error
	ModifyQPToRTR(qp VerbsQP, destQPN uint32, destLID uint16, destGID []byte, port int) error
	ModifyQPToRTS(qp VerbsQP) error
	QueryQP(qp VerbsQP) (*VerbsQPAttr, error)

	// Memory Registration
	RegMR(pd VerbsPD, addr uintptr, length int, access int) (VerbsMR, error)
	DeregMR(mr VerbsMR) error

	// Work Requests
	PostSend(qp VerbsQP, wr *VerbsSendWR) error
	PostRecv(qp VerbsQP, wr *VerbsRecvWR) error

	// RDMA Operations
	PostRDMARead(qp VerbsQP, localAddr, remoteAddr uintptr, length int, lkey, rkey uint32) error
	PostRDMAWrite(qp VerbsQP, localAddr, remoteAddr uintptr, length int, lkey, rkey uint32) error

	// Metrics
	GetMetrics() map[string]interface{}
}

// Handle types for verbs objects.
type VerbsContext uintptr
type VerbsPD uintptr
type VerbsCQ uintptr
type VerbsQP uintptr
type VerbsMR uintptr

// QPType represents queue pair types.
type QPType int

const (
	QPTypeRC  QPType = iota // Reliable Connection
	QPTypeUC                // Unreliable Connection
	QPTypeUD                // Unreliable Datagram
	QPTypeXRC               // Extended Reliable Connection
)

// Memory region access flags.
const (
	MRAccessLocalWrite   = 1 << 0
	MRAccessRemoteWrite  = 1 << 1
	MRAccessRemoteRead   = 1 << 2
	MRAccessRemoteAtomic = 1 << 3
)

// Work completion status.
type WCStatus int

const (
	WCSuccess WCStatus = iota
	WCLocalLenErr
	WCLocalQPOpErr
	WCLocalEECOpErr
	WCLocalProtErr
	WCWRFlushErr
	WCMWBindErr
	WCBadRespErr
	WCLocalAccessErr
	WCRemoteInvalidReqErr
	WCRemoteAccessErr
	WCRemoteOpErr
	WCRetryExcErr
	WCRnrRetryExcErr
	WCLocalRddViolErr
	WCRemoteInvalidRdReqErr
	WCRemoteAbortedErr
	WCInvEECNErr
	WCInvEECStateErr
	WCFatalErr
	WCRespTimeoutErr
	WCGeneralErr
)

// Work completion opcode.
type WCOpcode int

const (
	WCOpSend WCOpcode = iota
	WCOpRDMAWrite
	WCOpRDMARead
	WCOpCompSwap
	WCOpFetchAdd
	WCOpBindMW
	WCOpLocalInv
	WCOpRecv
	WCOpRecvRDMAWithImm
)

// VerbsDeviceInfo contains RDMA device information.
type VerbsDeviceInfo struct {
	Name         string
	FWVer        string
	GUID         uint64
	NodeType     int
	Transport    int
	PhysPortCnt  int
	VendorID     uint32
	VendorPartID uint32
	HWVer        uint32
}

// VerbsWorkCompletion represents a work completion entry.
type VerbsWorkCompletion struct {
	WRID      uint64
	Status    WCStatus
	Opcode    WCOpcode
	VendorErr uint32
	ByteLen   uint32
	ImmData   uint32
	QPN       uint32
	SrcQP     uint32
	WCFlags   int
	PkeyIndex uint16
	SLID      uint16
	SL        uint8
	DLIDPath  uint8
}

// VerbsQPAttr contains queue pair attributes.
type VerbsQPAttr struct {
	State           int
	CurState        int
	Path            VerbsAHAttr
	AltPath         VerbsAHAttr
	QPN             uint32
	DestQPN         uint32
	QKey            uint32
	RQPsn           uint32
	SQPsn           uint32
	DestQKey        uint32
	QPAccessFlags   int
	Cap             VerbsQPCap
	MaxRdAtomic     uint8
	MaxDestRdAtomic uint8
	MinRnrTimer     uint8
	PortNum         uint8
	Timeout         uint8
	RetryCnt        uint8
	RnrRetry        uint8
	AltPortNum      uint8
	AltTimeout      uint8
}

// VerbsAHAttr contains address handle attributes.
type VerbsAHAttr struct {
	GRH         VerbsGlobalRoute
	DLID        uint16
	SL          uint8
	SrcPathBits uint8
	StaticRate  uint8
	IsGlobal    uint8
	PortNum     uint8
}

// VerbsGlobalRoute contains global routing info.
type VerbsGlobalRoute struct {
	DGID         [16]byte
	FlowLabel    uint32
	SGIDIX       uint8
	HopLimit     uint8
	TrafficClass uint8
}

// VerbsQPCap contains queue pair capabilities.
type VerbsQPCap struct {
	MaxSendWR     uint32
	MaxRecvWR     uint32
	MaxSendSge    uint32
	MaxRecvSge    uint32
	MaxInlineData uint32
}

// VerbsSendWR represents a send work request.
type VerbsSendWR struct {
	Next       *VerbsSendWR
	SGList     []VerbsSGE
	WRID       uint64
	Opcode     int
	SendFlags  int
	RemoteAddr uint64
	ImmData    uint32
	RKey       uint32
}

// VerbsRecvWR represents a receive work request.
type VerbsRecvWR struct {
	Next   *VerbsRecvWR
	SGList []VerbsSGE
	WRID   uint64
}

// VerbsSGE represents a scatter/gather entry.
type VerbsSGE struct {
	Addr   uint64
	Length uint32
	LKey   uint32
}

// SimulatedVerbsBackend provides a simulated libibverbs implementation for testing.
type SimulatedVerbsBackend struct {
	contexts    map[VerbsContext]*simulatedContext
	pds         map[VerbsPD]*simulatedPD
	cqs         map[VerbsCQ]*simulatedCQ
	qps         map[VerbsQP]*simulatedQP
	mrs         map[VerbsMR]*simulatedMR
	completions chan VerbsWorkCompletion
	metrics     *verbsMetrics
	devices     []VerbsDeviceInfo
	nextHandle  uintptr
	mu          sync.RWMutex
	initialized bool
}

type simulatedContext struct {
	device *VerbsDeviceInfo
}

type simulatedPD struct {
	ctx VerbsContext
}

type simulatedCQ struct {
	completions []VerbsWorkCompletion
	ctx         VerbsContext
	size        int
}

type simulatedQP struct {
	pd      VerbsPD
	sendCQ  VerbsCQ
	recvCQ  VerbsCQ
	qpType  QPType
	qpNum   uint32
	state   int
	maxSend int
	maxRecv int
	maxSge  int
}

type simulatedMR struct {
	pd     VerbsPD
	addr   uintptr
	length int
	access int
	lkey   uint32
	rkey   uint32
}

type verbsMetrics struct {
	DevicesOpened int64
	PDsCreated    int64
	CQsCreated    int64
	QPsCreated    int64
	MRsRegistered int64
	SendsPosted   int64
	RecvsPosted   int64
	RDMAReads     int64
	RDMAWrites    int64
	Completions   int64
	Errors        int64
}

// NewSimulatedVerbsBackend creates a new simulated verbs backend.
func NewSimulatedVerbsBackend() *SimulatedVerbsBackend {
	return &SimulatedVerbsBackend{
		contexts:    make(map[VerbsContext]*simulatedContext),
		pds:         make(map[VerbsPD]*simulatedPD),
		cqs:         make(map[VerbsCQ]*simulatedCQ),
		qps:         make(map[VerbsQP]*simulatedQP),
		mrs:         make(map[VerbsMR]*simulatedMR),
		completions: make(chan VerbsWorkCompletion, 1000),
		metrics:     &verbsMetrics{},
	}
}

func (b *SimulatedVerbsBackend) Init() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.initialized {
		return nil
	}

	// Create simulated RDMA devices
	b.devices = []VerbsDeviceInfo{
		{
			Name:         "mlx5_0",
			GUID:         0xDEADBEEF00000001,
			NodeType:     1,      // CA
			Transport:    1,      // InfiniBand
			VendorID:     0x15b3, // Mellanox
			VendorPartID: 0x1017, // ConnectX-6
			HWVer:        0,
			FWVer:        "20.35.1012",
			PhysPortCnt:  2,
		},
		{
			Name:         "mlx5_1",
			GUID:         0xDEADBEEF00000002,
			NodeType:     1,
			Transport:    1,
			VendorID:     0x15b3,
			VendorPartID: 0x1017,
			HWVer:        0,
			FWVer:        "20.35.1012",
			PhysPortCnt:  2,
		},
	}

	b.initialized = true

	return nil
}

func (b *SimulatedVerbsBackend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.contexts = make(map[VerbsContext]*simulatedContext)
	b.pds = make(map[VerbsPD]*simulatedPD)
	b.cqs = make(map[VerbsCQ]*simulatedCQ)
	b.qps = make(map[VerbsQP]*simulatedQP)
	b.mrs = make(map[VerbsMR]*simulatedMR)
	b.initialized = false

	return nil
}

func (b *SimulatedVerbsBackend) GetDeviceList() ([]VerbsDeviceInfo, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if !b.initialized {
		return nil, ErrVerbsNotInitialized
	}

	result := make([]VerbsDeviceInfo, len(b.devices))
	copy(result, b.devices)

	return result, nil
}

func (b *SimulatedVerbsBackend) OpenDevice(name string) (VerbsContext, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.initialized {
		return 0, ErrVerbsNotInitialized
	}

	var device *VerbsDeviceInfo

	for i := range b.devices {
		if b.devices[i].Name == name {
			device = &b.devices[i]
			break
		}
	}

	if device == nil {
		return 0, ErrDeviceNotFound
	}

	b.nextHandle++
	ctx := VerbsContext(b.nextHandle)
	b.contexts[ctx] = &simulatedContext{device: device}
	atomic.AddInt64(&b.metrics.DevicesOpened, 1)

	return ctx, nil
}

func (b *SimulatedVerbsBackend) CloseDevice(ctx VerbsContext) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	delete(b.contexts, ctx)

	return nil
}

func (b *SimulatedVerbsBackend) AllocPD(ctx VerbsContext) (VerbsPD, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.contexts[ctx]; !ok {
		return 0, ErrContextCreation
	}

	b.nextHandle++
	pd := VerbsPD(b.nextHandle)
	b.pds[pd] = &simulatedPD{ctx: ctx}
	atomic.AddInt64(&b.metrics.PDsCreated, 1)

	return pd, nil
}

func (b *SimulatedVerbsBackend) DeallocPD(pd VerbsPD) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	delete(b.pds, pd)

	return nil
}

func (b *SimulatedVerbsBackend) CreateCQ(ctx VerbsContext, cqe int) (VerbsCQ, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.contexts[ctx]; !ok {
		return 0, ErrContextCreation
	}

	b.nextHandle++
	cq := VerbsCQ(b.nextHandle)
	b.cqs[cq] = &simulatedCQ{
		ctx:         ctx,
		size:        cqe,
		completions: make([]VerbsWorkCompletion, 0),
	}
	atomic.AddInt64(&b.metrics.CQsCreated, 1)

	return cq, nil
}

func (b *SimulatedVerbsBackend) DestroyCQ(cq VerbsCQ) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	delete(b.cqs, cq)

	return nil
}

func (b *SimulatedVerbsBackend) PollCQ(cq VerbsCQ, numEntries int) ([]VerbsWorkCompletion, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	simCQ, ok := b.cqs[cq]
	if !ok {
		return nil, ErrCQCreation
	}

	// Return any queued completions
	count := numEntries
	if len(simCQ.completions) < count {
		count = len(simCQ.completions)
	}

	result := simCQ.completions[:count]
	simCQ.completions = simCQ.completions[count:]

	atomic.AddInt64(&b.metrics.Completions, int64(len(result)))

	return result, nil
}

func (b *SimulatedVerbsBackend) CreateQP(pd VerbsPD, sendCQ, recvCQ VerbsCQ, qpType QPType, maxSend, maxRecv, maxSge int) (VerbsQP, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.pds[pd]; !ok {
		return 0, ErrPDCreation
	}

	b.nextHandle++
	qp := VerbsQP(b.nextHandle)
	b.qps[qp] = &simulatedQP{
		pd:      pd,
		sendCQ:  sendCQ,
		recvCQ:  recvCQ,
		qpType:  qpType,
		qpNum:   uint32(b.nextHandle),
		state:   0, // RESET
		maxSend: maxSend,
		maxRecv: maxRecv,
		maxSge:  maxSge,
	}
	atomic.AddInt64(&b.metrics.QPsCreated, 1)

	return qp, nil
}

func (b *SimulatedVerbsBackend) DestroyQP(qp VerbsQP) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	delete(b.qps, qp)

	return nil
}

func (b *SimulatedVerbsBackend) ModifyQPToInit(qp VerbsQP, port int) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	simQP, ok := b.qps[qp]
	if !ok {
		return ErrQPCreation
	}

	simQP.state = 1 // INIT

	return nil
}

func (b *SimulatedVerbsBackend) ModifyQPToRTR(qp VerbsQP, destQPN uint32, destLID uint16, destGID []byte, port int) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	simQP, ok := b.qps[qp]
	if !ok {
		return ErrQPCreation
	}

	simQP.state = 2 // RTR

	return nil
}

func (b *SimulatedVerbsBackend) ModifyQPToRTS(qp VerbsQP) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	simQP, ok := b.qps[qp]
	if !ok {
		return ErrQPCreation
	}

	simQP.state = 3 // RTS

	return nil
}

func (b *SimulatedVerbsBackend) QueryQP(qp VerbsQP) (*VerbsQPAttr, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	simQP, ok := b.qps[qp]
	if !ok {
		return nil, ErrQPCreation
	}

	return &VerbsQPAttr{
		State: simQP.state,
		QPN:   simQP.qpNum,
		Cap: VerbsQPCap{
			MaxSendWR:  uint32(simQP.maxSend),  //nolint:gosec // G115: maxSend bounded by QP config
			MaxRecvWR:  uint32(simQP.maxRecv),  //nolint:gosec // G115: maxRecv bounded by QP config
			MaxSendSge: uint32(simQP.maxSge),   //nolint:gosec // G115: maxSge bounded by QP config
			MaxRecvSge: uint32(simQP.maxSge),   //nolint:gosec // G115: maxSge bounded by QP config
		},
	}, nil
}

func (b *SimulatedVerbsBackend) RegMR(pd VerbsPD, addr uintptr, length int, access int) (VerbsMR, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.pds[pd]; !ok {
		return 0, ErrPDCreation
	}

	b.nextHandle++
	mr := VerbsMR(b.nextHandle)
	b.mrs[mr] = &simulatedMR{
		pd:     pd,
		addr:   addr,
		length: length,
		access: access,
		lkey:   uint32(b.nextHandle),
		rkey:   uint32(b.nextHandle),
	}
	atomic.AddInt64(&b.metrics.MRsRegistered, 1)

	return mr, nil
}

func (b *SimulatedVerbsBackend) DeregMR(mr VerbsMR) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	delete(b.mrs, mr)

	return nil
}

func (b *SimulatedVerbsBackend) PostSend(qp VerbsQP, wr *VerbsSendWR) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	simQP, ok := b.qps[qp]
	if !ok {
		return ErrQPCreation
	}

	// Simulate completion
	simCQ, ok := b.cqs[simQP.sendCQ]
	if ok {
		simCQ.completions = append(simCQ.completions, VerbsWorkCompletion{
			WRID:    wr.WRID,
			Status:  WCSuccess,
			Opcode:  WCOpSend,
			ByteLen: wr.SGList[0].Length,
		})
	}

	atomic.AddInt64(&b.metrics.SendsPosted, 1)

	return nil
}

func (b *SimulatedVerbsBackend) PostRecv(qp VerbsQP, wr *VerbsRecvWR) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	_, ok := b.qps[qp]
	if !ok {
		return ErrQPCreation
	}

	atomic.AddInt64(&b.metrics.RecvsPosted, 1)

	return nil
}

func (b *SimulatedVerbsBackend) PostRDMARead(qp VerbsQP, localAddr, remoteAddr uintptr, length int, lkey, rkey uint32) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	simQP, ok := b.qps[qp]
	if !ok {
		return ErrQPCreation
	}

	// Simulate completion
	simCQ, ok := b.cqs[simQP.sendCQ]
	if ok {
		simCQ.completions = append(simCQ.completions, VerbsWorkCompletion{
			//nolint:gosec // G115: UnixNano returns positive values for current timestamps
			WRID:    uint64(time.Now().UnixNano()),
			Status:  WCSuccess,
			Opcode:  WCOpRDMARead,
			ByteLen: uint32(length), //nolint:gosec // G115: length is bounded by RDMA hardware limits
		})
	}

	atomic.AddInt64(&b.metrics.RDMAReads, 1)

	return nil
}

func (b *SimulatedVerbsBackend) PostRDMAWrite(qp VerbsQP, localAddr, remoteAddr uintptr, length int, lkey, rkey uint32) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	simQP, ok := b.qps[qp]
	if !ok {
		return ErrQPCreation
	}

	// Simulate completion
	simCQ, ok := b.cqs[simQP.sendCQ]
	if ok {
		simCQ.completions = append(simCQ.completions, VerbsWorkCompletion{
			//nolint:gosec // G115: UnixNano returns positive values for current timestamps
			WRID:    uint64(time.Now().UnixNano()),
			Status:  WCSuccess,
			Opcode:  WCOpRDMAWrite,
			ByteLen: uint32(length), //nolint:gosec // G115: length is bounded by RDMA hardware limits
		})
	}

	atomic.AddInt64(&b.metrics.RDMAWrites, 1)

	return nil
}

func (b *SimulatedVerbsBackend) GetMetrics() map[string]interface{} {
	return map[string]interface{}{
		"simulated":      true,
		"devices_opened": atomic.LoadInt64(&b.metrics.DevicesOpened),
		"pds_created":    atomic.LoadInt64(&b.metrics.PDsCreated),
		"cqs_created":    atomic.LoadInt64(&b.metrics.CQsCreated),
		"qps_created":    atomic.LoadInt64(&b.metrics.QPsCreated),
		"mrs_registered": atomic.LoadInt64(&b.metrics.MRsRegistered),
		"sends_posted":   atomic.LoadInt64(&b.metrics.SendsPosted),
		"recvs_posted":   atomic.LoadInt64(&b.metrics.RecvsPosted),
		"rdma_reads":     atomic.LoadInt64(&b.metrics.RDMAReads),
		"rdma_writes":    atomic.LoadInt64(&b.metrics.RDMAWrites),
		"completions":    atomic.LoadInt64(&b.metrics.Completions),
		"errors":         atomic.LoadInt64(&b.metrics.Errors),
	}
}

// VerbsTransport wraps VerbsBackend with higher-level operations.
type VerbsTransport struct {
	backend VerbsBackend
	config  *VerbsConfig
	ctx     VerbsContext
	pd      VerbsPD
	sendCQ  VerbsCQ
	recvCQ  VerbsCQ
	qp      VerbsQP
	mu      sync.RWMutex
}

// VerbsConfig holds configuration for the verbs transport.
type VerbsConfig struct {
	DeviceName string
	Port       int
	CQSize     int
	MaxSendWR  int
	MaxRecvWR  int
	MaxSGE     int
	QPType     QPType
}

// DefaultVerbsConfig returns default configuration.
func DefaultVerbsConfig() *VerbsConfig {
	return &VerbsConfig{
		DeviceName: "mlx5_0",
		Port:       1,
		CQSize:     256,
		MaxSendWR:  128,
		MaxRecvWR:  128,
		MaxSGE:     4,
		QPType:     QPTypeRC,
	}
}

// NewVerbsTransport creates a new verbs transport.
func NewVerbsTransport(backend VerbsBackend, config *VerbsConfig) (*VerbsTransport, error) {
	if backend == nil {
		backend = NewSimulatedVerbsBackend()
	}

	if config == nil {
		config = DefaultVerbsConfig()
	}

	err := backend.Init()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize verbs backend: %w", err)
	}

	return &VerbsTransport{
		backend: backend,
		config:  config,
	}, nil
}

// Connect establishes an RDMA connection to a remote endpoint.
func (t *VerbsTransport) Connect(ctx context.Context, remoteQPN uint32, remoteLID uint16, remoteGID []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Open device
	deviceCtx, err := t.backend.OpenDevice(t.config.DeviceName)
	if err != nil {
		return fmt.Errorf("failed to open device: %w", err)
	}

	t.ctx = deviceCtx

	// Create protection domain
	pd, err := t.backend.AllocPD(deviceCtx)
	if err != nil {
		return fmt.Errorf("failed to allocate PD: %w", err)
	}

	t.pd = pd

	// Create completion queues
	sendCQ, err := t.backend.CreateCQ(deviceCtx, t.config.CQSize)
	if err != nil {
		return fmt.Errorf("failed to create send CQ: %w", err)
	}

	t.sendCQ = sendCQ

	recvCQ, err := t.backend.CreateCQ(deviceCtx, t.config.CQSize)
	if err != nil {
		return fmt.Errorf("failed to create recv CQ: %w", err)
	}

	t.recvCQ = recvCQ

	// Create queue pair
	qp, err := t.backend.CreateQP(pd, sendCQ, recvCQ, t.config.QPType,
		t.config.MaxSendWR, t.config.MaxRecvWR, t.config.MaxSGE)
	if err != nil {
		return fmt.Errorf("failed to create QP: %w", err)
	}

	t.qp = qp

	// Transition QP to Init
	if err := t.backend.ModifyQPToInit(qp, t.config.Port); err != nil {
		return fmt.Errorf("failed to modify QP to Init: %w", err)
	}

	// Transition QP to RTR
	if err := t.backend.ModifyQPToRTR(qp, remoteQPN, remoteLID, remoteGID, t.config.Port); err != nil {
		return fmt.Errorf("failed to modify QP to RTR: %w", err)
	}

	// Transition QP to RTS
	if err := t.backend.ModifyQPToRTS(qp); err != nil {
		return fmt.Errorf("failed to modify QP to RTS: %w", err)
	}

	return nil
}

// Close shuts down the transport.
func (t *VerbsTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.qp != 0 {
		_ = t.backend.DestroyQP(t.qp)
	}

	if t.sendCQ != 0 {
		_ = t.backend.DestroyCQ(t.sendCQ)
	}

	if t.recvCQ != 0 {
		_ = t.backend.DestroyCQ(t.recvCQ)
	}

	if t.pd != 0 {
		_ = t.backend.DeallocPD(t.pd)
	}

	if t.ctx != 0 {
		_ = t.backend.CloseDevice(t.ctx)
	}

	return t.backend.Close()
}

// GetQPN returns the local queue pair number.
func (t *VerbsTransport) GetQPN() (uint32, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.qp == 0 {
		return 0, ErrQPCreation
	}

	attr, err := t.backend.QueryQP(t.qp)
	if err != nil {
		return 0, err
	}

	return attr.QPN, nil
}

// RegisterMemory registers a memory region for RDMA operations.
func (t *VerbsTransport) RegisterMemory(addr uintptr, length int, access int) (VerbsMR, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.pd == 0 {
		return 0, ErrPDCreation
	}

	return t.backend.RegMR(t.pd, addr, length, access)
}

// DeregisterMemory deregisters a memory region.
func (t *VerbsTransport) DeregisterMemory(mr VerbsMR) error {
	return t.backend.DeregMR(mr)
}

// RDMARead performs an RDMA read operation.
func (t *VerbsTransport) RDMARead(localAddr, remoteAddr uintptr, length int, lkey, rkey uint32) error {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.qp == 0 {
		return ErrQPCreation
	}

	return t.backend.PostRDMARead(t.qp, localAddr, remoteAddr, length, lkey, rkey)
}

// RDMAWrite performs an RDMA write operation.
func (t *VerbsTransport) RDMAWrite(localAddr, remoteAddr uintptr, length int, lkey, rkey uint32) error {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.qp == 0 {
		return ErrQPCreation
	}

	return t.backend.PostRDMAWrite(t.qp, localAddr, remoteAddr, length, lkey, rkey)
}

// PollCompletions polls for work completions.
func (t *VerbsTransport) PollCompletions(numEntries int) ([]VerbsWorkCompletion, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.sendCQ == 0 {
		return nil, ErrCQCreation
	}

	return t.backend.PollCQ(t.sendCQ, numEntries)
}

// GetMetrics returns transport metrics.
func (t *VerbsTransport) GetMetrics() map[string]interface{} {
	return t.backend.GetMetrics()
}
