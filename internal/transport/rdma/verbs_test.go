package rdma

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSimulatedVerbsBackend(t *testing.T) {
	backend := NewSimulatedVerbsBackend()
	require.NotNil(t, backend)

	err := backend.Init()
	require.NoError(t, err)

	defer backend.Close()
}

func TestSimulatedVerbsBackendDoubleInit(t *testing.T) {
	backend := NewSimulatedVerbsBackend()

	err := backend.Init()
	require.NoError(t, err)

	// Double init should be ok
	err = backend.Init()
	require.NoError(t, err)

	err = backend.Close()
	require.NoError(t, err)
}

func TestSimulatedVerbsBackendGetDeviceList(t *testing.T) {
	backend := NewSimulatedVerbsBackend()

	backend.Init()
	defer backend.Close()

	devices, err := backend.GetDeviceList()
	require.NoError(t, err)
	require.Len(t, devices, 2)

	assert.Equal(t, "mlx5_0", devices[0].Name)
	assert.Equal(t, "mlx5_1", devices[1].Name)
	assert.Equal(t, uint32(0x15b3), devices[0].VendorID) // Mellanox
}

func TestSimulatedVerbsBackendNotInitialized(t *testing.T) {
	backend := NewSimulatedVerbsBackend()

	_, err := backend.GetDeviceList()
	assert.ErrorIs(t, err, ErrVerbsNotInitialized)

	_, err = backend.OpenDevice("mlx5_0")
	assert.ErrorIs(t, err, ErrVerbsNotInitialized)
}

func TestSimulatedVerbsBackendOpenDevice(t *testing.T) {
	backend := NewSimulatedVerbsBackend()

	backend.Init()
	defer backend.Close()

	ctx, err := backend.OpenDevice("mlx5_0")
	require.NoError(t, err)
	assert.NotZero(t, ctx)

	err = backend.CloseDevice(ctx)
	assert.NoError(t, err)
}

func TestSimulatedVerbsBackendOpenDeviceNotFound(t *testing.T) {
	backend := NewSimulatedVerbsBackend()

	backend.Init()
	defer backend.Close()

	_, err := backend.OpenDevice("nonexistent")
	assert.ErrorIs(t, err, ErrDeviceNotFound)
}

func TestSimulatedVerbsBackendAllocPD(t *testing.T) {
	backend := NewSimulatedVerbsBackend()

	backend.Init()
	defer backend.Close()

	ctx, _ := backend.OpenDevice("mlx5_0")
	defer backend.CloseDevice(ctx)

	pd, err := backend.AllocPD(ctx)
	require.NoError(t, err)
	assert.NotZero(t, pd)

	err = backend.DeallocPD(pd)
	assert.NoError(t, err)
}

func TestSimulatedVerbsBackendCreateCQ(t *testing.T) {
	backend := NewSimulatedVerbsBackend()

	backend.Init()
	defer backend.Close()

	ctx, _ := backend.OpenDevice("mlx5_0")
	defer backend.CloseDevice(ctx)

	cq, err := backend.CreateCQ(ctx, 256)
	require.NoError(t, err)
	assert.NotZero(t, cq)

	err = backend.DestroyCQ(cq)
	assert.NoError(t, err)
}

func TestSimulatedVerbsBackendCreateQP(t *testing.T) {
	backend := NewSimulatedVerbsBackend()

	backend.Init()
	defer backend.Close()

	ctx, _ := backend.OpenDevice("mlx5_0")
	defer backend.CloseDevice(ctx)

	pd, _ := backend.AllocPD(ctx)
	defer backend.DeallocPD(pd)

	sendCQ, _ := backend.CreateCQ(ctx, 256)
	defer backend.DestroyCQ(sendCQ)

	recvCQ, _ := backend.CreateCQ(ctx, 256)
	defer backend.DestroyCQ(recvCQ)

	qp, err := backend.CreateQP(pd, sendCQ, recvCQ, QPTypeRC, 128, 128, 4)
	require.NoError(t, err)
	assert.NotZero(t, qp)

	err = backend.DestroyQP(qp)
	assert.NoError(t, err)
}

func TestSimulatedVerbsBackendModifyQP(t *testing.T) {
	backend := NewSimulatedVerbsBackend()

	backend.Init()
	defer backend.Close()

	ctx, _ := backend.OpenDevice("mlx5_0")
	defer backend.CloseDevice(ctx)

	pd, _ := backend.AllocPD(ctx)
	defer backend.DeallocPD(pd)

	sendCQ, _ := backend.CreateCQ(ctx, 256)
	defer backend.DestroyCQ(sendCQ)

	recvCQ, _ := backend.CreateCQ(ctx, 256)
	defer backend.DestroyCQ(recvCQ)

	qp, _ := backend.CreateQP(pd, sendCQ, recvCQ, QPTypeRC, 128, 128, 4)
	defer backend.DestroyQP(qp)

	// Transition through states
	err := backend.ModifyQPToInit(qp, 1)
	require.NoError(t, err)

	err = backend.ModifyQPToRTR(qp, 12345, 1, nil, 1)
	require.NoError(t, err)

	err = backend.ModifyQPToRTS(qp)
	require.NoError(t, err)

	// Query QP
	attr, err := backend.QueryQP(qp)
	require.NoError(t, err)
	assert.Equal(t, 3, attr.State) // RTS
}

func TestSimulatedVerbsBackendRegMR(t *testing.T) {
	backend := NewSimulatedVerbsBackend()

	backend.Init()
	defer backend.Close()

	ctx, _ := backend.OpenDevice("mlx5_0")
	defer backend.CloseDevice(ctx)

	pd, _ := backend.AllocPD(ctx)
	defer backend.DeallocPD(pd)

	mr, err := backend.RegMR(pd, 0x1000, 4096, MRAccessLocalWrite|MRAccessRemoteRead)
	require.NoError(t, err)
	assert.NotZero(t, mr)

	err = backend.DeregMR(mr)
	assert.NoError(t, err)
}

func TestSimulatedVerbsBackendPostSend(t *testing.T) {
	backend := NewSimulatedVerbsBackend()

	backend.Init()
	defer backend.Close()

	ctx, _ := backend.OpenDevice("mlx5_0")
	defer backend.CloseDevice(ctx)

	pd, _ := backend.AllocPD(ctx)
	defer backend.DeallocPD(pd)

	sendCQ, _ := backend.CreateCQ(ctx, 256)
	defer backend.DestroyCQ(sendCQ)

	recvCQ, _ := backend.CreateCQ(ctx, 256)
	defer backend.DestroyCQ(recvCQ)

	qp, _ := backend.CreateQP(pd, sendCQ, recvCQ, QPTypeRC, 128, 128, 4)
	defer backend.DestroyQP(qp)

	wr := &VerbsSendWR{
		WRID: 1,
		SGList: []VerbsSGE{
			{Addr: 0x1000, Length: 1024, LKey: 1},
		},
	}

	err := backend.PostSend(qp, wr)
	require.NoError(t, err)

	// Poll for completion
	completions, err := backend.PollCQ(sendCQ, 10)
	require.NoError(t, err)
	require.Len(t, completions, 1)
	assert.Equal(t, WCSuccess, completions[0].Status)
}

// rdmaPostFunc is a function type for RDMA read/write post operations.
type rdmaPostFunc func(qp VerbsQP, localAddr, remoteAddr uintptr, length int, lkey, rkey uint32) error

// testRDMAOperation is a helper for testing RDMA read/write operations.
func testRDMAOperation(t *testing.T, postFn func(*SimulatedVerbsBackend) rdmaPostFunc, expectedOpcode WCOpcode) {
	t.Helper()

	backend := NewSimulatedVerbsBackend()
	backend.Init()

	defer backend.Close()

	ctx, _ := backend.OpenDevice("mlx5_0")
	defer backend.CloseDevice(ctx)

	pd, _ := backend.AllocPD(ctx)
	defer backend.DeallocPD(pd)

	sendCQ, _ := backend.CreateCQ(ctx, 256)
	defer backend.DestroyCQ(sendCQ)

	recvCQ, _ := backend.CreateCQ(ctx, 256)
	defer backend.DestroyCQ(recvCQ)

	qp, _ := backend.CreateQP(pd, sendCQ, recvCQ, QPTypeRC, 128, 128, 4)
	defer backend.DestroyQP(qp)

	op := postFn(backend)
	err := op(qp, 0x1000, 0x2000, 4096, 1, 2)
	require.NoError(t, err)

	completions, err := backend.PollCQ(sendCQ, 10)
	require.NoError(t, err)
	require.Len(t, completions, 1)
	assert.Equal(t, expectedOpcode, completions[0].Opcode)
}

func TestSimulatedVerbsBackendRDMARead(t *testing.T) {
	testRDMAOperation(t, func(b *SimulatedVerbsBackend) rdmaPostFunc {
		return b.PostRDMARead
	}, WCOpRDMARead)
}

func TestSimulatedVerbsBackendRDMAWrite(t *testing.T) {
	testRDMAOperation(t, func(b *SimulatedVerbsBackend) rdmaPostFunc {
		return b.PostRDMAWrite
	}, WCOpRDMAWrite)
}

func TestSimulatedVerbsBackendGetMetrics(t *testing.T) {
	backend := NewSimulatedVerbsBackend()

	backend.Init()
	defer backend.Close()

	ctx, _ := backend.OpenDevice("mlx5_0")
	pd, _ := backend.AllocPD(ctx)
	sendCQ, _ := backend.CreateCQ(ctx, 256)
	recvCQ, _ := backend.CreateCQ(ctx, 256)
	qp, _ := backend.CreateQP(pd, sendCQ, recvCQ, QPTypeRC, 128, 128, 4)

	// Post some operations
	backend.PostRDMARead(qp, 0x1000, 0x2000, 4096, 1, 2)
	backend.PostRDMAWrite(qp, 0x1000, 0x2000, 4096, 1, 2)

	metrics := backend.GetMetrics()
	assert.True(t, metrics["simulated"].(bool))
	assert.Equal(t, int64(1), metrics["devices_opened"])
	assert.Equal(t, int64(1), metrics["pds_created"])
	assert.Equal(t, int64(2), metrics["cqs_created"])
	assert.Equal(t, int64(1), metrics["qps_created"])
	assert.Equal(t, int64(1), metrics["rdma_reads"])
	assert.Equal(t, int64(1), metrics["rdma_writes"])

	backend.DestroyQP(qp)
	backend.DestroyCQ(sendCQ)
	backend.DestroyCQ(recvCQ)
	backend.DeallocPD(pd)
	backend.CloseDevice(ctx)
}

// VerbsTransport Tests

func TestNewVerbsTransport(t *testing.T) {
	transport, err := NewVerbsTransport(nil, nil)
	require.NoError(t, err)

	require.NotNil(t, transport)
	defer transport.Close()
}

func TestVerbsTransportWithBackend(t *testing.T) {
	backend := NewSimulatedVerbsBackend()
	config := DefaultVerbsConfig()

	transport, err := NewVerbsTransport(backend, config)
	require.NoError(t, err)

	require.NotNil(t, transport)
	defer transport.Close()
}

func TestVerbsTransportConnect(t *testing.T) {
	backend := NewSimulatedVerbsBackend()
	config := DefaultVerbsConfig()

	transport, err := NewVerbsTransport(backend, config)
	require.NoError(t, err)

	defer transport.Close()

	ctx := context.Background()
	err = transport.Connect(ctx, 12345, 1, nil)
	require.NoError(t, err)

	qpn, err := transport.GetQPN()
	require.NoError(t, err)
	assert.NotZero(t, qpn)
}

func TestVerbsTransportRDMAOperations(t *testing.T) {
	backend := NewSimulatedVerbsBackend()
	config := DefaultVerbsConfig()

	transport, err := NewVerbsTransport(backend, config)
	require.NoError(t, err)

	defer transport.Close()

	ctx := context.Background()
	err = transport.Connect(ctx, 12345, 1, nil)
	require.NoError(t, err)

	// Register memory
	mr, err := transport.RegisterMemory(0x1000, 4096, MRAccessLocalWrite|MRAccessRemoteRead)
	require.NoError(t, err)
	assert.NotZero(t, mr)

	// RDMA Read
	err = transport.RDMARead(0x1000, 0x2000, 4096, 1, 2)
	require.NoError(t, err)

	// RDMA Write
	err = transport.RDMAWrite(0x1000, 0x2000, 4096, 1, 2)
	require.NoError(t, err)

	// Poll completions
	completions, err := transport.PollCompletions(10)
	require.NoError(t, err)
	assert.Len(t, completions, 2)

	// Deregister memory
	err = transport.DeregisterMemory(mr)
	require.NoError(t, err)
}

func TestVerbsTransportGetMetrics(t *testing.T) {
	backend := NewSimulatedVerbsBackend()
	config := DefaultVerbsConfig()

	transport, err := NewVerbsTransport(backend, config)
	require.NoError(t, err)

	defer transport.Close()

	ctx := context.Background()
	transport.Connect(ctx, 12345, 1, nil)
	transport.RDMARead(0x1000, 0x2000, 4096, 1, 2)

	metrics := transport.GetMetrics()
	assert.NotNil(t, metrics)
	assert.True(t, metrics["simulated"].(bool))
}

func TestDefaultVerbsConfig(t *testing.T) {
	config := DefaultVerbsConfig()

	assert.Equal(t, "mlx5_0", config.DeviceName)
	assert.Equal(t, 1, config.Port)
	assert.Equal(t, 256, config.CQSize)
	assert.Equal(t, 128, config.MaxSendWR)
	assert.Equal(t, 128, config.MaxRecvWR)
	assert.Equal(t, 4, config.MaxSGE)
	assert.Equal(t, QPTypeRC, config.QPType)
}

func TestQPTypes(t *testing.T) {
	tests := []struct {
		qpType QPType
		value  int
	}{
		{QPTypeRC, 0},
		{QPTypeUC, 1},
		{QPTypeUD, 2},
		{QPTypeXRC, 3},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.value, int(tt.qpType))
	}
}

func TestMRAccessFlags(t *testing.T) {
	assert.Equal(t, 1, MRAccessLocalWrite)
	assert.Equal(t, 2, MRAccessRemoteWrite)
	assert.Equal(t, 4, MRAccessRemoteRead)
	assert.Equal(t, 8, MRAccessRemoteAtomic)
}

func TestWCStatus(t *testing.T) {
	assert.Equal(t, WCSuccess, WCStatus(0))
	assert.Equal(t, WCLocalLenErr, WCStatus(1))
}

func TestWCOpcode(t *testing.T) {
	assert.Equal(t, WCOpSend, WCOpcode(0))
	assert.Equal(t, WCOpRDMAWrite, WCOpcode(1))
	assert.Equal(t, WCOpRDMARead, WCOpcode(2))
}
